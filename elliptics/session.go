package elliptics

import (
	"fmt"
	"unsafe"
)

/*
#include "session.h"
#include <stdio.h>
*/
import "C"

var _ = fmt.Scanf

const VOLUME = 10

//Result of remove
type IRemoveResult interface {
	Error() error
}

type removeResult struct {
	err error
}

func (r *removeResult) Error() error {
	return r.err
}

//Session
type Session struct {
	session unsafe.Pointer
}

func NewSession(node *Node) (*Session, error) {
	session, err := C.new_elliptics_session(node.node)
	if err != nil {
		return nil, err
	}
	return &Session{session}, err
}

//Set groups to the session
func (s *Session) SetGroups(groups []int32) {
	C.session_set_groups(s.session, (*C.int32_t)(&groups[0]), C.int(len(groups)))
}

//Set namespace for elliptics session.
//Default namespace is empty string.
func (s *Session) SetNamespace(namespace string) {
	cnamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cnamespace))
	C.session_set_namespace(s.session, cnamespace, C.int(len(namespace)))
}

/*
	Read
*/

type ReadResult interface {
	Data() string
	Error() error
}

type readResult struct {
	ioAttr C.struct_dnet_io_attr
	data   string
	err    error
}

func (r *readResult) Data() string {
	return r.data
}

func (r *readResult) Error() error {
	return r.err
}

func (s *Session) ReadKey(key *Key) <-chan ReadResult {
	//Context is closure, which contains channel to answer in.
	//It will pass as the last argument to exported go_*_callback
	//through C++ callback after operation finish comes.
	//go_read_callback casts context to properly go func,
	//and calls with []ReadResult
	responseCh := make(chan ReadResult, VOLUME)
	onResult := func(result readResult) {
		responseCh <- &result
	}

	onFinish := func(err int) {
		if err != 0 {
			responseCh <- &readResult{err: fmt.Errorf("%d", err)}
		}
		close(responseCh)
	}
	C.session_read_data(s.session,
		unsafe.Pointer(&onResult), unsafe.Pointer(&onFinish),
		key.key)
	return responseCh
}

func (s *Session) ReadData(key string) <-chan ReadResult {
	ekey, err := NewKey(key)
	if err != nil {
		errCh := make(chan ReadResult, 1)
		errCh <- &readResult{err: err}
		close(errCh)
		return errCh
	}
	defer ekey.Free()
	return s.ReadKey(ekey)
}

/*
	Write and Lookup
*/

type Lookuper interface {
	Path() string
	Addr() C.struct_dnet_addr
	Info() C.struct_dnet_file_info
	Error() error
}

type lookupResult struct {
	info C.struct_dnet_file_info //dnet_file_info
	addr C.struct_dnet_addr
	path string //file_path
	err  error
}

func (l *lookupResult) Path() string {
	return l.path
}

func (l *lookupResult) Addr() C.struct_dnet_addr {
	return l.addr
}

func (l *lookupResult) Info() C.struct_dnet_file_info {
	return l.info
}

func (l *lookupResult) Error() error {
	return l.err
}

func (s *Session) WriteData(key string, blob string) <-chan Lookuper {
	ekey, err := NewKey(key)
	if err != nil {
		responseCh := make(chan Lookuper, VOLUME)
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}
	defer ekey.Free()
	return s.WriteKey(ekey, blob)
}

func (s *Session) WriteKey(key *Key, blob string) <-chan Lookuper {
	responseCh := make(chan Lookuper, VOLUME)
	raw_data := C.CString(blob) // Mustn't call free. Elliptics does it.

	onResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onFinish := func(err int) {
		if err != 0 {
			responseCh <- &lookupResult{err: fmt.Errorf("%d", err)}
		}
		close(responseCh)
	}

	C.session_write_data(s.session,
		unsafe.Pointer(&onResult), unsafe.Pointer(&onFinish),
		key.key, raw_data, C.size_t(len(blob)))
	return responseCh
}

func (s *Session) Lookup(key *Key) <-chan Lookuper {
	responseCh := make(chan Lookuper, VOLUME)

	onResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onFinish := func(err int) {
		if err != 0 {
			responseCh <- &lookupResult{err: fmt.Errorf("%d", err)}
		}
		close(responseCh)
	}

	C.session_lookup(s.session, unsafe.Pointer(&onResult), unsafe.Pointer(&onFinish), key.key)
	return responseCh
}

/*
	Remove
*/

func (s *Session) Remove(key string) (responseCh chan IRemoveResult) {
	ekey, err := NewKey(key)
	if err != nil {
		return
	}
	defer ekey.Free()
	return s.RemoveKey(ekey)
}

func (s *Session) RemoveKey(key *Key) (responseCh chan IRemoveResult) {
	responseCh = make(chan IRemoveResult, VOLUME)
	context := func(err int) {
		responseCh <- &removeResult{err: fmt.Errorf("%v", err)}
	}

	C.session_remove(s.session, unsafe.Pointer(&context), key.key)
	return
}

/*
	Find
*/

type Finder interface {
	Error() error
	Data() []IndexEntry
}

type FindResult struct {
	id   C.struct_dnet_raw_id
	data []IndexEntry
	err  error
}

type IndexEntry struct {
	Data string
}

func (f *FindResult) Data() []IndexEntry {
	return f.data
}

func (f *FindResult) Error() error {
	return f.err
}

func (s *Session) FindAllIndexes(indexes []string) <-chan Finder {
	responseCh := make(chan Finder, VOLUME)
	onResult, onFinish, cindexes := s.findIndexes(indexes, responseCh)
	C.session_find_all_indexes(s.session, onResult, onFinish,
		(**C.char)(&cindexes[0]), C.size_t(len(indexes)))
	return responseCh
}

func (s *Session) FindAnyIndexes(indexes []string) <-chan Finder {
	responseCh := make(chan Finder, VOLUME)
	onResult, onFinish, cindexes := s.findIndexes(indexes, responseCh)
	C.session_find_any_indexes(s.session, onResult, onFinish,
		(**C.char)(&cindexes[0]), C.size_t(len(indexes)))
	return responseCh
}

func (s *Session) findIndexes(indexes []string, responseCh chan Finder) (onResult, onFinish unsafe.Pointer, cindexes []*C.char) {
	for _, index := range indexes {
		cindex := C.CString(index)
		cindexes = append(cindexes, cindex)
	}

	_result := func(result *FindResult) {
		responseCh <- result
	}
	onResult = unsafe.Pointer(&_result)

	_finish := func(err int) {
		if err != 0 {
			responseCh <- &FindResult{err: fmt.Errorf("%d", err)}
		}
		close(responseCh)
	}
	onFinish = unsafe.Pointer(&_finish)
	return
}
