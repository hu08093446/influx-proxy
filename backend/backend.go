// Copyright 2021 Shiwen Cheng. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"io"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
)

type CacheBuffer struct {
	Buffer  *bytes.Buffer
	Counter int
}

// Backend 每个Backend对应一个InfluxDB示例
type Backend struct {
	*HttpBackend
	fb   *FileBackend
	pool *ants.Pool

	running         atomic.Value
	flushSize       int
	flushTime       int
	rewriteInterval int
	rewriteTicker   *time.Ticker
	chWrite         chan *LinePoint
	// 目前看这个chTimer的作用是这样的：每次write请求在经过一致性hash分发之后不是立刻发送给influxDB实例的，而是在缓存里累积到一定数量才发送的，那如果发送速率比较慢
	// 数量达不到，请求就会一直放在缓存里发布出去，这个时候就需要有时间限制，隔一段时间检查一次，保证累积在缓存中但是总数量没有达标的请求在延迟
	// 较短的时间后也能发送出去   它这里没有使用定时器而使用这种方式是有道理的，个人认为是降低资源消耗，因为只要请求数量达标，这个chTimer就不会
	// 被触发，只有在请求比较缓慢的时候才可能被触发。这个设计还是比较巧妙地。
	chTimer <-chan time.Time
	buffers map[string]map[string]*CacheBuffer
	wg      sync.WaitGroup
}

func NewBackend(cfg *BackendConfig, pxcfg *ProxyConfig) (ib *Backend) {
	ib = &Backend{
		HttpBackend:     NewHttpBackend(cfg, pxcfg),
		flushSize:       pxcfg.FlushSize,
		flushTime:       pxcfg.FlushTime,
		rewriteInterval: pxcfg.RewriteInterval,
		// 这里的定时器是定时检查有没有本地文件生成（请求的失败的时候会把请求写入文件），然后将文件中记录的请求进行重放
		rewriteTicker: time.NewTicker(time.Duration(pxcfg.RewriteInterval) * time.Second),
		chWrite:       make(chan *LinePoint, 16),
		// 这里是个双层map，类似于Java中的Map<String, Map<String, *CacheBuffer>>
		buffers: make(map[string]map[string]*CacheBuffer),
	}
	ib.running.Store(true)

	var err error
	ib.fb, err = NewFileBackend(cfg.Name, pxcfg.DataDir)
	if err != nil {
		panic(err)
	}
	ib.pool, err = ants.NewPool(pxcfg.ConnPoolSize)
	if err != nil {
		panic(err)
	}

	go ib.worker()
	return
}

func NewSimpleBackend(cfg *BackendConfig) *Backend {
	return &Backend{HttpBackend: NewSimpleHttpBackend(cfg)}
}

func (ib *Backend) worker() {
	for ib.IsRunning() {
		select {
		case p, ok := <-ib.chWrite:
			if !ok {
				// closed
				ib.Flush()
				ib.wg.Wait()
				ib.HttpBackend.Close()
				ib.fb.Close()
				ib.pool.Release()
				return
			}
			ib.WriteBuffer(p)

		case <-ib.chTimer:
			ib.Flush()
			if !ib.IsRunning() {
				ib.wg.Wait()
				ib.HttpBackend.Close()
				ib.fb.Close()
				ib.pool.Release()
				return
			}

		case <-ib.rewriteTicker.C:
			ib.RewriteIdle()
		}
	}
}

func (ib *Backend) WritePoint(point *LinePoint) (err error) {
	if !ib.IsRunning() {
		return io.ErrClosedPipe
	}
	ib.chWrite <- point
	return
}

func (ib *Backend) WriteBuffer(point *LinePoint) (err error) {
	db, rp, line := point.Db, point.Rp, point.Line
	// it's thread-safe since ib.buffers is only used (read-write) in ib.worker() goroutine
	if _, ok := ib.buffers[db]; !ok {
		ib.buffers[db] = make(map[string]*CacheBuffer)
	}
	// 不同的保存策略要分开存储，不能混在一起
	if _, ok := ib.buffers[db][rp]; !ok {
		ib.buffers[db][rp] = &CacheBuffer{Buffer: &bytes.Buffer{}}
	}
	cb := ib.buffers[db][rp]
	cb.Counter++
	if cb.Buffer == nil {
		cb.Buffer = &bytes.Buffer{}
	}
	n, err := cb.Buffer.Write(line)
	if err != nil {
		log.Printf("buffer write error: %s", err)
		return
	}
	if n != len(line) {
		err = io.ErrShortWrite
		log.Printf("buffer write error: %s", err)
		return
	}
	if line[len(line)-1] != '\n' {
		err = cb.Buffer.WriteByte('\n')
		if err != nil {
			log.Printf("buffer write error: %s", err)
			return
		}
	}

	switch {
	case cb.Counter >= ib.flushSize:
		ib.FlushBuffer(db, rp)
	case ib.chTimer == nil:
		ib.chTimer = time.After(time.Duration(ib.flushTime) * time.Second)
	}
	return
}

func (ib *Backend) FlushBuffer(db, rp string) {
	cb := ib.buffers[db][rp]
	if cb.Buffer == nil {
		return
	}
	p := cb.Buffer.Bytes()
	// 这里不存在线程安全问题，因为这里的执行者都是同一个协程
	cb.Buffer = nil
	cb.Counter = 0
	if len(p) == 0 {
		return
	}

	ib.wg.Add(1)
	// 这里使用了ants协程库，当pool中的协程都在忙碌时，此时提交任务会阻塞
	ib.pool.Submit(func() {
		defer ib.wg.Done()
		var buf bytes.Buffer
		err := Compress(&buf, p)
		if err != nil {
			log.Print("compress buffer error: ", err)
			return
		}

		p = buf.Bytes()

		if ib.IsActive() {
			err = ib.WriteCompressed(db, rp, p)
			switch err {
			case nil:
				return
			case ErrBadRequest:
				log.Printf("bad request, drop all data")
				return
			case ErrNotFound:
				log.Printf("bad backend, drop all data")
				return
			default:
				log.Printf("write http error: %s %s %s, length: %d", ib.Url, db, rp, len(p))
			}
		}

		b := bytes.Join([][]byte{[]byte(url.QueryEscape(db)), []byte(url.QueryEscape(rp)), p}, []byte{' '})
		// 如果调用backend接口出现问题，这里会将请求写入到文件中
		err = ib.fb.Write(b)
		if err != nil {
			log.Printf("write db and data to file error with db: %s, rp: %s, length: %d error: %s", db, rp, len(p), err)
			return
		}
	})
}

func (ib *Backend) Flush() {
	ib.chTimer = nil
	for db := range ib.buffers {
		for rp := range ib.buffers[db] {
			if ib.buffers[db][rp].Counter > 0 {
				ib.FlushBuffer(db, rp)
			}
		}
	}
}

func (ib *Backend) RewriteIdle() {
	// 这里的rewriting可以保证：如果上一次重写没有写完，那么下一次不会开始
	if !ib.IsRewriting() && ib.fb.IsData() {
		ib.SetRewriting(true)
		go ib.RewriteLoop()
	}
}

func (ib *Backend) RewriteLoop() {
	for ib.fb.IsData() {
		if !ib.IsRunning() {
			return
		}
		if !ib.IsActive() {
			time.Sleep(time.Duration(ib.rewriteInterval) * time.Second)
			continue
		}
		err := ib.Rewrite()
		if err != nil {
			time.Sleep(time.Duration(ib.rewriteInterval) * time.Second)
			continue
		}
	}
	ib.SetRewriting(false)
}

func (ib *Backend) Rewrite() (err error) {
	b, err := ib.fb.Read()
	if err != nil {
		log.Print("rewrite read file error: ", err)
		return
	}
	if b == nil {
		return
	}

	p := bytes.SplitN(b, []byte{' '}, 3)
	if len(p) < 3 {
		log.Print("rewrite read invalid data with length: ", len(p))
		return
	}
	db, err := url.QueryUnescape(string(p[0]))
	if err != nil {
		log.Print("rewrite db unescape error: ", err)
		return
	}
	rp, err := url.QueryUnescape(string(p[1]))
	if err != nil {
		log.Print("rewrite rp unescape error: ", err)
		return
	}
	err = ib.WriteCompressed(db, rp, p[2])

	switch err {
	case nil:
	case ErrBadRequest:
		log.Printf("bad request, drop all data")
		err = nil
	case ErrNotFound:
		log.Printf("bad backend, drop all data")
		err = nil
	default:
		log.Printf("rewrite http error: %s %s %s, length: %d", ib.Url, db, rp, len(p[1]))

		err = ib.fb.RollbackMeta()
		if err != nil {
			log.Printf("rollback meta error: %s", err)
		}
		return
	}

	err = ib.fb.UpdateMeta()
	if err != nil {
		log.Printf("update meta error: %s", err)
	}
	return
}

func (ib *Backend) IsRunning() (b bool) {
	return ib.running.Load().(bool)
}

func (ib *Backend) Close() {
	ib.running.Store(false)
	close(ib.chWrite)
}

func (ib *Backend) GetHealth(ic *Circle, withStats bool) interface{} {
	health := struct {
		Name      string      `json:"name"`
		Url       string      `json:"url"` // nolint:golint
		Active    bool        `json:"active"`
		Backlog   bool        `json:"backlog"`
		Rewriting bool        `json:"rewriting"`
		WriteOnly bool        `json:"write_only"`
		Healthy   bool        `json:"healthy,omitempty"`
		Stats     interface{} `json:"stats,omitempty"`
	}{
		Name:      ib.Name,
		Url:       ib.Url,
		Active:    ib.IsActive(),
		Backlog:   ib.fb.IsData(),
		Rewriting: ib.IsRewriting(),
		WriteOnly: ib.IsWriteOnly(),
	}
	if !withStats {
		return health
	}
	var wg sync.WaitGroup
	var smap sync.Map
	dbs := ib.GetDatabases()
	for _, db := range dbs {
		wg.Add(1)
		go func(db string) {
			defer wg.Done()
			inplace, incorrect := 0, 0
			measurements := ib.GetMeasurements(db)
			for _, meas := range measurements {
				key := GetKey(db, meas)
				nb := ic.GetBackend(key)
				if nb.Url == ib.Url {
					inplace++
				} else {
					incorrect++
				}
			}
			// smap是一个嵌套的线程安全的map，key是string，value是map[string]int
			smap.Store(db, map[string]int{
				"measurements": len(measurements),
				"inplace":      inplace,
				"incorrect":    incorrect,
			})
		}(db)
	}
	wg.Wait()
	healthy := true
	// 为什么这里要新建一个stats，直接用smap不好么？
	// 个人理解：stats这个map是要供外面使用的，而smap是上面多协程并发操纵用的，如果把smap直接返回了，那外部的协程和这里的协程就要互相争夺资源了
	// 在能保证线程安全的情况下，需要加锁的东西要尽可能少的使用
	stats := make(map[string]map[string]int)
	smap.Range(func(k, v interface{}) bool {
		// 下面这种写法是类型转换，将interface{}类型转为map
		sm := v.(map[string]int)
		stats[k.(string)] = sm
		if sm["incorrect"] > 0 {
			healthy = false
		}
		return true
	})
	health.Healthy = healthy
	health.Stats = stats
	return health
}
