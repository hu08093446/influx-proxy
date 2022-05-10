// Copyright 2021 Shiwen Cheng. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package util

// 这里使用map自定义了一个类似Jav的set类型
// non-thread-safe set
type Set map[string]bool

func NewSet(items ...string) Set {
	set := make(Set)
	for _, s := range items {
		set[s] = true
	}
	return set
}

func NewSetFromSlice(slice []string) Set {
	return NewSet(slice...)
}

func (set Set) Add(s string) {
	set[s] = true
}

func (set Set) Remove(s string) {
	delete(set, s)
}
