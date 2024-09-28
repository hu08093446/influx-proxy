// Copyright 2021 Shiwen Cheng. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package backend

import (
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type FileBackend struct {
	lock     sync.Mutex
	filename string
	datadir  string
	dataflag bool
	producer *os.File
	consumer *os.File
	meta     *os.File
}

func NewFileBackend(filename string, datadir string) (fb *FileBackend, err error) {
	fb = &FileBackend{
		filename: filename,
		datadir:  datadir,
	}

	pathname := filepath.Join(datadir, filename)
	// note 生产者只需要不断地在dat文件末尾添加数据就行了，所以采用append模式
	fb.producer, err = os.OpenFile(pathname+".dat", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Printf("open producer error: %s %s", fb.filename, err)
		return
	}

	// note 消费者对dat文件只读不写，读取的位移记录在rec文件中
	fb.consumer, err = os.OpenFile(pathname+".dat", os.O_RDONLY, 0644)
	if err != nil {
		log.Printf("open consumer error: %s %s", fb.filename, err)
		return
	}

	fb.meta, err = os.OpenFile(pathname+".rec", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Printf("open meta error: %s %s", fb.filename, err)
		return
	}

	// note 这个方法中会设置消费者的位移位置（如果是全新的rec文件，则不会设置）
	fb.RollbackMeta()
	// note 生产者就直接从末尾开始
	producerOffset, _ := fb.producer.Seek(0, io.SeekEnd)
	offset, _ := fb.consumer.Seek(0, io.SeekCurrent)
	// note 生产者的位移大于消费者，说明dat中有数据没有消费，此时对dataFlag进行设置
	fb.dataflag = producerOffset > offset
	return
}

func (fb *FileBackend) Write(p []byte) (err error) {
	fb.lock.Lock()
	defer fb.lock.Unlock()

	var length = uint32(len(p))
	err = binary.Write(fb.producer, binary.BigEndian, length)
	if err != nil {
		log.Print("write length error: ", err)
		return
	}

	n, err := fb.producer.Write(p)
	if err != nil {
		log.Print("write error: ", err)
		return
	}
	if n != len(p) {
		return io.ErrShortWrite
	}

	err = fb.producer.Sync()
	if err != nil {
		log.Print("sync meta error: ", err)
		return
	}

	// note 设置标识，标识文件里有新的数据需要处理了
	fb.dataflag = true
	return
}

func (fb *FileBackend) IsData() bool {
	fb.lock.Lock()
	defer fb.lock.Unlock()
	return fb.dataflag
}

func (fb *FileBackend) Read() (p []byte, err error) {
	if !fb.IsData() {
		return nil, nil
	}
	var length uint32

	err = binary.Read(fb.consumer, binary.BigEndian, &length)
	if err != nil {
		log.Print("read length error: ", err)
		return
	}
	p = make([]byte, length)

	_, err = io.ReadFull(fb.consumer, p)
	if err != nil {
		log.Print("read error: ", err)
		return
	}
	return
}

func (fb *FileBackend) RollbackMeta() (err error) {
	fb.lock.Lock()
	defer fb.lock.Unlock()

	_, err = fb.meta.Seek(0, io.SeekStart)
	if err != nil {
		log.Printf("seek meta error: %s %s", fb.filename, err)
		return
	}

	var offset int64
	err = binary.Read(fb.meta, binary.BigEndian, &offset)
	if err != nil {
		// note 最开始第一次读取的时候，meta文件是空的，所以会返回EOF错误
		if err != io.EOF {
			log.Printf("read meta error: %s %s", fb.filename, err)
		}
		return
	}
	// note meta中记录的是消费的offset，所以拿到offset后设置当前的消费位移
	// note 那为什么没有生产的offset呢？我理解的是，生产只需要在dat文件末尾添加就行了，不需要记录什么offset
	_, err = fb.consumer.Seek(offset, io.SeekStart)
	if err != nil {
		log.Printf("seek consumer error: %s %s", fb.filename, err)
		return
	}
	return
}

func (fb *FileBackend) UpdateMeta() (err error) {
	fb.lock.Lock()
	defer fb.lock.Unlock()

	producerOffset, err := fb.producer.Seek(0, io.SeekCurrent)
	if err != nil {
		log.Printf("seek producer error: %s %s", fb.filename, err)
		return
	}

	offset, err := fb.consumer.Seek(0, io.SeekCurrent)
	if err != nil {
		log.Printf("seek consumer error: %s %s", fb.filename, err)
		return
	}

	if producerOffset == offset {
		err = fb.CleanUp()
		if err != nil {
			log.Printf("cleanup error: %s %s", fb.filename, err)
			return
		}
		offset = 0
	}

	_, err = fb.meta.Seek(0, io.SeekStart)
	if err != nil {
		log.Printf("seek meta error: %s %s", fb.filename, err)
		return
	}

	log.Printf("write meta: %s, %d", fb.filename, offset)
	err = binary.Write(fb.meta, binary.BigEndian, &offset)
	if err != nil {
		log.Printf("write meta error: %s %s", fb.filename, err)
		return
	}

	err = fb.meta.Sync()
	if err != nil {
		log.Printf("sync meta error: %s %s", fb.filename, err)
		return
	}

	return
}

func (fb *FileBackend) CleanUp() (err error) {
	_, err = fb.consumer.Seek(0, io.SeekStart)
	if err != nil {
		log.Print("seek consumer error: ", err)
		return
	}
	filename := filepath.Join(fb.datadir, fb.filename+".dat")
	err = os.Truncate(filename, 0)
	if err != nil {
		log.Print("truncate error: ", err)
		return
	}
	err = fb.producer.Close()
	if err != nil {
		log.Print("close producer error: ", err)
		return
	}
	fb.producer, err = os.OpenFile(filename, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Print("open producer error: ", err)
		return
	}
	fb.dataflag = false
	return
}

func (fb *FileBackend) Close() {
	fb.producer.Close()
	fb.consumer.Close()
	fb.meta.Close()
}
