/*
 * 2013+ Copyright (c) Anton Tyurin <noxiouz@yandex.ru>
 * 2014+ Copyright (c) Evgeniy Polyakov <zbr@ioremap.net>
 * All rights reserved.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 */

package elliptics

/*
#include "session.h"
#include <stdio.h>
*/
import "C"


import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"unsafe"
	"errors"
)

const defaultVOLUME = 10
const max_chunk_size uint64 = 10 * 1024 * 1024

const (
	indexesSet = iota
	indexesUpdate
)

/*Session allows to perfom any operations with data and indexes.

Most of methods return channel. Channel will be closed after results end or
error occurs. In case of error last value received from channel returns non nil value
from Error method.

For example Remove:

    if rm, ok := <-session.Remove(KEY); !ok {
        //Remove normally doesn't return any value, so chanel was closed.
        log.Println("Remove successfully")
    } else {
        //We's received value from channel. It should be error message.
        log.Println("Error occured: ", rm.Error())
    }
*/

func makeLookuperChan(err error, volume int) chan Lookuper {
	if volume == 0 {
		volume = defaultVOLUME
	}
	responseCh := make(chan Lookuper, volume)
	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
	}
	return responseCh

}

type Session struct {
	groups  []uint32
	session unsafe.Pointer
}

//NewSession returns Session connected with given Node.
func NewSession(node *Node) (*Session, error) {
	session, err := C.new_elliptics_session(node.node)
	if err != nil {
		return nil, err
	}
	return &Session{
		session: session,
		groups:  make([]uint32, 0, 0),
	}, err
}

func (s *Session) Delete() {
	C.delete_session(s.session)
}

//SetGroups points groups Session should work with.
func (s *Session) SetGroups(groups []uint32) {
	C.session_set_groups(s.session, (*C.uint32_t)(&groups[0]), C.int(len(groups)))
	s.groups = groups
}

//GetGroups returns array of groups this session holds
func (s *Session) GetGroups() []uint32 {
	return s.groups
}

//SetTimeout sets wait timeout in seconds (time to wait for operation to complete) for all subsequent session operations
func (s *Session) SetTimeout(timeout int) {
	// replace C.int to C.long as soon as fix is done in elliptics
	C.session_set_timeout(s.session, C.int(timeout))
}

func (s *Session) GetTimeout() int {
	return int(C.session_get_timeout(s.session))
}

//SetCflags sets command flags (DNET_FLAGS_* in API documentation) like nolock
func (s *Session) SetCflags(cflags Cflag) {
	C.session_set_cflags(s.session, C.cflags_t(cflags))
}

func (s *Session) GetCflags() Cflag {
	return Cflag(C.session_get_cflags(s.session))
}

//SetIOflags sets IO flags (DNET_IO_FLAGS_* in API documentation), i.e. flags for IO operations like read/write/delete
func (s *Session) SetIOflags(ioflags IOflag) {
	C.session_set_ioflags(s.session, C.ioflags_t(ioflags))
}

func (s *Session) GetIOflags() IOflag {
	return IOflag(C.session_get_ioflags(s.session))
}

func (s *Session) SetTraceID(trace TraceID) {
	C.session_set_trace_id(s.session, C.trace_id_t(trace))
}

func (s *Session) GetTraceID() TraceID {
	return TraceID(C.session_get_trace_id(s.session))
}

/*
 * @SetNamespace sets the namespace for the Session. Default namespace is empty string.
 *
 * This feature allows you to share a single storage between services.
 * And each service which uses own namespace will have own independent space of keys.
 */
func (s *Session) SetNamespace(namespace string) {
	cnamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cnamespace))
	C.session_set_namespace(s.session, cnamespace, C.int(len(namespace)))
}

const (
	SessionFilterAll      = iota
	SessionFilterPositive = iota
	SessionFilterMax      = iota
)

func (s *Session) SetFilter(filter int) {
	if filter >= SessionFilterMax {
		return
	}

	switch filter {
	case SessionFilterAll:
		C.session_set_filter_all(s.session)
	case SessionFilterPositive:
		C.session_set_filter_positive(s.session)
	}
}

/*
   Read
*/

//ReadResult wraps one result of read operation.
type ReadResult interface {
	// server's reply
	Cmd() *DnetCmd

	// server's address
	Addr() *DnetAddr

	// IO parameters for given
	IO() *DnetIOAttr

	//Data returns string represntation of read data
	Data() []byte

	// read error
	Error() error

	Key() string
}

type readResult struct {
	cmd    DnetCmd
	addr   DnetAddr
	ioattr DnetIOAttr
	data   []byte
	err    error
	key    string
}

func (r *readResult) Cmd() *DnetCmd {
	return &r.cmd
}
func (r *readResult) Addr() *DnetAddr {
	return &r.addr
}
func (r *readResult) IO() *DnetIOAttr {
	return &r.ioattr
}
func (r *readResult) Data() []byte {
	return r.data
}
func (r *readResult) Error() error {
	return r.err
}

func (r *readResult) Key() string {
	return r.key
}

//StreamData sends a stream read from elliptics into given http response writer
// It doesn't start reading next chunk (10M) until the one already read has not been written
// into the client's pipe. This eliminates number of unneeded copies and adds flow control
// of the client's pips.
func (s *Session) StreamHTTP(kstr string, offset, size uint64, w http.ResponseWriter) error {
	key, err := NewKey(kstr)
	if err != nil {
		return err
	}
	defer key.Free()

	orig_offset := offset
	orig_size := size

	// size == 0 means 'read everything
	for size >= 0 {
		chunk_size := size
		if chunk_size > max_chunk_size || chunk_size == 0 {
			chunk_size = max_chunk_size
		}

		err = &DnetError{
			Code:    -6,
			Flags:   0,
			Message: fmt.Sprintf("could not read anything at all"),
		}

		for rd := range s.ReadKey(key, offset, chunk_size) {
			err = rd.Error()
			if err != nil {
				continue
			}

			if offset == orig_offset {
				if size == 0 || size > rd.IO().TotalSize-offset {
					size = rd.IO().TotalSize - offset
				}

				w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
				w.WriteHeader(http.StatusOK)
			}

			data := rd.Data()

			w.Write(data)

			offset += uint64(len(data))
			size -= uint64(len(data))
			break
		}

		if err != nil {
			return &DnetError{
				Code:  ErrorStatus(err),
				Flags: 0,
				Message: fmt.Sprintf("could not stream data: current-offset: %d/%d, current-size: %d, rest-size: %d/%d: %v",
					orig_offset, offset, chunk_size, orig_size, size, err),
			}
		}

		if size == 0 {
			break
		}
	}

	return nil
}

//ReadKey performs a read operation by key.
func (s *Session) ReadKey(key *Key, offset, size uint64) <-chan ReadResult {
	responseCh := make(chan ReadResult, defaultVOLUME)
	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(result *readResult) {

		responseCh <- result
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &readResult{err: err}
		}

		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)

	C.session_read_data(s.session,
		C.context_t(onResultContext), C.context_t(onFinishContext),
		key.key, C.uint64_t(offset), C.uint64_t(size))
	return responseCh
}

func (s *Session) BulkRead(keys []string) ( <- chan ReadResult) {
	ekeys, err := NewKeys(keys)
	if err != nil {
		errCh := make(chan ReadResult, 1)
		errCh <- &readResult{err: err}
		close(errCh)
		ekeys.Free()
		return errCh
	}

	responseCh := make(chan ReadResult, defaultVOLUME)
	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(result *readResult) {
		key, err := ekeys.Find(result.Cmd().ID.ID)
		if err != nil {
			return
		}
		result.key = key
		responseCh <- result
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &readResult{err: err}
		}

		close(responseCh)
		ekeys.Free()
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)

	C.session_bulk_read(s.session,
		C.context_t(onResultContext), C.context_t(onFinishContext),
		ekeys.keys,
	)
	return responseCh

}

//ReadKey performs a read operation by string representation of key.
func (s *Session) ReadData(key string, offset, size uint64) <-chan ReadResult {
	ekey, err := NewKey(key)
	if err != nil {
		errCh := make(chan ReadResult, 1)
		errCh <- &readResult{err: err}
		close(errCh)
		return errCh
	}
	defer ekey.Free()
	return s.ReadKey(ekey, offset, size)
}

/*
   Write and Lookup
*/

//Lookuper represents one result of Write and Lookup operations.
type Lookuper interface {
	// server's reply
	Cmd() *DnetCmd

	// server's address
	Addr() *DnetAddr

	// dnet_file_info structure contains basic information about key location
	Info() *DnetFileInfo

	// address of the node which hosts given key
	StorageAddr() *DnetAddr

	//Path returns a path to file hosting given key on the storage.
	Path() string

	//Error returns string respresentation of error.
	Error() error
	Key() string
}

type lookupResult struct {
	cmd          DnetCmd
	addr         DnetAddr
	info         DnetFileInfo
	storage_addr DnetAddr
	path         string
	err          error
	key          string
}

func (l *lookupResult) Cmd() *DnetCmd {
	return &l.cmd
}
func (l *lookupResult) Addr() *DnetAddr {
	return &l.addr
}
func (l *lookupResult) Info() *DnetFileInfo {
	return &l.info
}
func (l *lookupResult) StorageAddr() *DnetAddr {
	return &l.storage_addr
}
func (l *lookupResult) Path() string {
	return l.path
}
func (l *lookupResult) Error() error {
	return l.err
}

func (l *lookupResult) Key() string {
	return l.key
}

//WriteData writes blob by a given string representation of Key.
func (s *Session) WriteData(key string, input io.Reader, offset, total_size uint64) <-chan Lookuper {
	if total_size > max_chunk_size {
		return s.WriteChunk(key, input, offset, total_size)
	}

	ekey, err := NewKey(key)
	if err != nil {
		return makeLookuperChan(err, defaultVOLUME)
	}
	defer ekey.Free()
	return s.WriteKey(ekey, input, offset, total_size)
}

func (s *Session) WriteChunk(key string, input io.Reader, initial_offset, total_size uint64) <-chan Lookuper {
	responseCh := make(chan Lookuper, defaultVOLUME)
	onChunkContext := NextContext()
	onFinishContext := NextContext()
	chunk_context := NextContext()

	chunk := make([]byte, max_chunk_size, max_chunk_size)

	orig_total_size := total_size
	offset := initial_offset
	var n64 uint64

	onChunkResult := func(lookup *lookupResult) {
		if total_size == 0 {
			responseCh <- lookup
		}
	}

	var onChunkFinish func(err error)

	onChunkFinish = func(err error) {
		if err != nil {
			responseCh <- &lookupResult{err: err}
			close(responseCh)
			Pool.Delete(onChunkContext)
			Pool.Delete(onFinishContext)
			Pool.Delete(chunk_context)
			return
		}

		if total_size == 0 {
			close(responseCh)
			Pool.Delete(onChunkContext)
			Pool.Delete(onFinishContext)
			Pool.Delete(chunk_context)
			return
		}

		n, err := input.Read(chunk)
		if n <= 0 && err != nil {
			responseCh <- &lookupResult{err: err}
			close(responseCh)
			Pool.Delete(onChunkContext)
			Pool.Delete(onFinishContext)
			Pool.Delete(chunk_context)
			return
		}

		n64 = uint64(n)
		total_size -= n64
		offset += n64

		ekey, err := NewKey(key)
		if err != nil {
			responseCh <- &lookupResult{err: err}
			close(responseCh)
			Pool.Delete(onChunkContext)
			Pool.Delete(onFinishContext)
			Pool.Delete(chunk_context)
			return
		}
		defer ekey.Free()

		if total_size != 0 {
			C.session_write_plain(s.session,
				C.context_t(onChunkContext), C.context_t(onFinishContext),
				ekey.key, C.uint64_t(offset-n64),
				(*C.char)(unsafe.Pointer(&chunk[0])), C.uint64_t(n))
		} else {
			C.session_write_commit(s.session,
				C.context_t(onChunkContext), C.context_t(onFinishContext),
				ekey.key, C.uint64_t(offset-n64), C.uint64_t(offset),
				(*C.char)(unsafe.Pointer(&chunk[0])), C.uint64_t(n))
		}
	}

	rest := total_size
	if rest > max_chunk_size {
		rest = max_chunk_size
	}

	n, err := input.Read(chunk)
	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}

	if n == 0 {
		responseCh <- &lookupResult{
			err: &DnetError{
				Code:  -22,
				Flags: 0,
				Message: fmt.Sprintf("Invalid zero-length write: current-offset: %d/%d, rest-size: %d/%d",
					initial_offset, offset, total_size, orig_total_size),
			},
		}
	}

	n64 = uint64(n)
	total_size -= n64
	offset += n64

	ekey, err := NewKey(key)
	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}
	defer ekey.Free()

	Pool.Store(onChunkContext, onChunkResult)
	Pool.Store(onFinishContext, onChunkFinish)
	Pool.Store(chunk_context, chunk)

	C.session_write_prepare(s.session,
		C.context_t(onChunkContext), C.context_t(onFinishContext),
		ekey.key, C.uint64_t(offset-n64), C.uint64_t(total_size+n64),
		(*C.char)(unsafe.Pointer(&chunk[0])), C.uint64_t(n))
	return responseCh
}

// WriteCache write blob by key to cache with expected timeout by seconds
func (s *Session) WriteCache(key string, input io.Reader, timeout uint64) <-chan Lookuper {
	ekey, err := NewKey(key)
	if err != nil {
		return makeLookuperChan(err, defaultVOLUME)
	}
	defer ekey.Free()

	responseCh := makeLookuperChan(nil, defaultVOLUME)

	onWriteContext := NextContext()
	onWriteFinishContext := NextContext()
	chunk_context := NextContext()

	onWriteResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onWriteFinish := func(err error) {
		if err != nil {
			responseCh <- &lookupResult{err: err}
		}
		close(responseCh)
		Pool.Delete(onWriteContext)
		Pool.Delete(onWriteFinishContext)
		Pool.Delete(chunk_context)
	}

	chunk, err := ioutil.ReadAll(input)

	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}

	if len(chunk) == 0 {
		responseCh <- &lookupResult{
			err: &DnetError{
				Code:    -22,
				Flags:   0,
				Message: "Invalid zero-length write request",
			},
		}
		close(responseCh)
		return responseCh
	}

	Pool.Store(onWriteContext, onWriteResult)
	Pool.Store(onWriteFinishContext, onWriteFinish)
	Pool.Store(chunk_context, chunk)

	C.session_write_cache(s.session,
		C.context_t(onWriteContext), C.context_t(onWriteFinishContext),
		ekey.key, (*C.char)(unsafe.Pointer(&chunk[0])),
		C.long(timeout), C.uint64_t(len(chunk)))

	return responseCh
}

//WriteKey writes blob by Key.
func (s *Session) WriteKey(key *Key, input io.Reader, offset, total_size uint64) <-chan Lookuper {
	responseCh := make(chan Lookuper, defaultVOLUME)
	onWriteContext := NextContext()
	onWriteFinishContext := NextContext()
	chunk_context := NextContext()

	onWriteResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onWriteFinish := func(err error) {
		if err != nil {
			responseCh <- &lookupResult{err: err}
		}
		close(responseCh)
		Pool.Delete(onWriteContext)
		Pool.Delete(onWriteFinishContext)
		Pool.Delete(chunk_context)
	}

	chunk, err := ioutil.ReadAll(input)
	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}

	if len(chunk) == 0 {
		responseCh <- &lookupResult{
			err: &DnetError{
				Code:    -22,
				Flags:   0,
				Message: "Invalid zero-length write request",
			},
		}
		close(responseCh)
		return responseCh
	}

	Pool.Store(onWriteContext, onWriteResult)
	Pool.Store(onWriteFinishContext, onWriteFinish)
	Pool.Store(chunk_context, chunk)

	C.session_write_data(s.session,
		C.context_t(onWriteContext), C.context_t(onWriteFinishContext),
		key.key, C.uint64_t(offset), (*C.char)(unsafe.Pointer(&chunk[0])), C.uint64_t(len(chunk)))

	return responseCh
}

// Lookup returns an information about given Key.
// It only returns the first group where key has been found.
func (s *Session) Lookup(key *Key) <-chan Lookuper {
	responseCh := make(chan Lookuper, defaultVOLUME)
	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &lookupResult{err: err}
		}
		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)

	C.session_lookup(s.session, C.context_t(onResultContext), C.context_t(onFinishContext), key.key)
	return responseCh
}

// ParallelLookup returns all information about given Key,
// it sends multiple lookup requests in parallel to all session groups
// and returns information about all specified group where given key has been found.
func (s *Session) ParallelLookup(kstr string) <-chan Lookuper {
	responseCh := make(chan Lookuper, defaultVOLUME)
	onResultContext := NextContext()
	onFinishContext := NextContext()

	key, err := NewKey(kstr)
	if err != nil {
		responseCh <- &lookupResult{err: err}
		close(responseCh)
		return responseCh
	}
	defer key.Free()

	onResult := func(lookup *lookupResult) {
		responseCh <- lookup
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &lookupResult{err: err}
		}
		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	/* To keep callbacks alive */
	C.session_parallel_lookup(s.session, C.context_t(onResultContext), C.context_t(onFinishContext), key.key)
	return responseCh
}

/*
   Remove
*/

//Remover wraps information about remove operation.
type Remover interface {
	// server's reply
	Cmd() *DnetCmd

	// key to be removed, only set for error results
	Key() string

	//Error of remove operation.
	Error() error
}

type removeResult struct {
	cmd DnetCmd
	key string
	err error
}

func (r *removeResult) Cmd() *DnetCmd {
	return &r.cmd
}
func (r *removeResult) Key() string {
	return r.key
}
func (r *removeResult) Error() error {
	return r.err
}

//Remove performs remove operation by a string.
func (s *Session) Remove(key string) <-chan Remover {
	ekey, err := NewKey(key)
	if err != nil {
		responseCh := make(chan Remover, defaultVOLUME)
		responseCh <- &removeResult{err: err}
		close(responseCh)
		return responseCh
	}
	defer ekey.Free()
	return s.RemoveKey(ekey)
}

//RemoveKey performs remove operation by key.
func (s *Session) RemoveKey(key *Key) <-chan Remover {
	responseCh := make(chan Remover, defaultVOLUME)
	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(r *removeResult) {
		responseCh <- r
	}
	onFinish := func(err error) {
		if err != nil {
			responseCh <- &removeResult{err: err}
		}
		close(responseCh)

		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	C.session_remove(s.session, C.context_t(onResultContext), C.context_t(onFinishContext), key.key)
	return responseCh
}

func (s *Session) BulkWrite(attrs []*DnetIOAttr, data [][]byte) (<- chan Lookuper) {
	bulk := C.ell_bulk_blobs_new()

	if bulk == nil {
		return  makeLookuperChan(errors.New("not allocated memory for blobs containter"), defaultVOLUME)
	}
	defer C.ell_bulk_blobs_free(bulk)
	if len(attrs) != len(data) {
		return  makeLookuperChan(errors.New("inconsistent input parametrs"), defaultVOLUME)
	}
	keys_l := make([]string, len(attrs))
	for i, attr := range attrs {
		keys_l[i] = string(attr.ID)
		ioattr, err := attr.ToIOAttr()
		if err != nil {
			return  makeLookuperChan(err, defaultVOLUME)
		}
		v := data[i]
		C.ell_bulk_blobs_insert(bulk, ioattr, (*C.char)(unsafe.Pointer(&v[0])), C.uint64_t(len(v)))
	}
	keys, err := NewKeys(keys_l)
	if err != nil {
		return  makeLookuperChan(err, defaultVOLUME)
	}



	/*for k, v := range dataset {
		//values[i] = v
		ioattr := new(DnetIOAttr)
		ioattr.ID = []byte(k)
		attr, err := ioattr.ToIOAttr()
		if err != nil {
			return  makeLookuperChan(err, defaultVOLUME)
		}
		C.ell_bulk_blobs_insert(bulk, attr, (*C.char)(unsafe.Pointer(&v[0])), C.uint64_t(len(v)))
	}*/

	responseCh := makeLookuperChan(nil, defaultVOLUME)

	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(r *lookupResult) {
		if r.err != nil {
			responseCh <- r
			return
		}

		key, err := keys.Find(r.Cmd().ID.ID)
		if err != nil {
			return
		}
		r.key = key
		responseCh <- r
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &lookupResult{
				err: err,
			}
		}

		close(responseCh)
		keys.Free()
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)

	C.session_bulk_write(s.session,
		C.context_t(onResultContext), C.context_t(onFinishContext),  bulk,
	)

	//(*C.char)(unsafe.Pointer(&chunk[0]))

	/*C.session_bulk_write(s.session,
		C.context_t(onResultContext), C.context_t(onFinishContext),
		(**C.ell_io_attr)(&keys[0]), (**C.char)(unsafe.Pointer(&)))*/
	return responseCh
}

//BulkRemove removes keys from array. It returns error for every key it could not delete.
func (s *Session) BulkRemove(keys_str []string) <-chan Remover {
	responseCh := make(chan Remover, defaultVOLUME)

	keys, err := NewKeys(keys_str)
	if err != nil {
		responseCh <- &removeResult{
			key: "new keys allocation failed",
			err: err,
		}
		close(responseCh)
		return responseCh
	}

	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(r *removeResult) {
		if r.err != nil {
			responseCh <- r
		} else if r.cmd.Status != 0 {

			key, err := keys.Find(r.Cmd().ID.ID)
			if err != nil {
				responseCh <- &removeResult{
					key: "could not find key for replied ID",
					err: err,
				}
				return
			}

			r.err = fmt.Errorf("remove status: %d", r.cmd.Status)
			r.key = key
			responseCh <- r
		}
	}
	onFinish := func(err error) {
		if err != nil {
			responseCh <- &removeResult{
				key: "overall operation result",
				err: err,
			}
		}

		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	C.session_bulk_remove(s.session, C.context_t(onResultContext), C.context_t(onFinishContext), keys.keys)

	return responseCh
}

/*
   Find
*/

//Finder is interface to result of find operations with Indexes.
type Finder interface {
	Error() error
	Data() []IndexEntry
}

type findResult struct {
	id   C.struct_dnet_raw_id
	data []IndexEntry
	err  error
}

//IndexEntry represents one result of some index operations.
type IndexEntry struct {
	//Data is information associated with index.
	Data string
	err  error
}

func (i *IndexEntry) Error() error {
	return i.err
}

func (f *findResult) Data() []IndexEntry {
	return f.data
}

func (f *findResult) Error() error {
	return f.err
}

//FindAllIndexes returns IndexEntries for keys, which were indexed with all of indexes.
func (s *Session) FindAllIndexes(indexes []string) <-chan Finder {
	responseCh := make(chan Finder, defaultVOLUME)
	onResult, onFinish, cindexes := s.findIndexes(indexes, responseCh)
	C.session_find_all_indexes(s.session, C.context_t(onResult), C.context_t(onFinish),
		(**C.char)(&cindexes[0]), C.uint64_t(len(indexes)))
	//Free cindexes
	for _, item := range cindexes {
		C.free(unsafe.Pointer(item))
	}
	return responseCh
}

//FindAnyIndexes returns IndexEntries for keys, which were indexed with any of indexes.
func (s *Session) FindAnyIndexes(indexes []string) <-chan Finder {
	responseCh := make(chan Finder, defaultVOLUME)
	onResult, onFinish, cindexes := s.findIndexes(indexes, responseCh)
	C.session_find_any_indexes(s.session, C.context_t(onResult), C.context_t(onFinish),
		(**C.char)(&cindexes[0]), C.uint64_t(len(indexes)))
	//Free cindexes
	for _, item := range cindexes {
		C.free(unsafe.Pointer(item))
	}
	return responseCh
}

func (s *Session) findIndexes(indexes []string, responseCh chan Finder) (onResultContext, onFinishContext uint64, cindexes []*C.char) {
	for _, index := range indexes {
		cindex := C.CString(index)
		cindexes = append(cindexes, cindex)
	}
	onResultContext = NextContext()
	onFinishContext = NextContext()

	onResult := func(result *findResult) {
		responseCh <- result
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &findResult{err: err}
		}
		close(responseCh)

		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	return
}

/*
   Indexes
*/

//Indexer is an interface to result of any CRUD operations with indexes.
type Indexer interface {
	//Error returns string representation of error.
	Error() error
}

type indexResult struct {
	err error
}

func (i *indexResult) Error() error {
	return i.err
}

func (s *Session) setOrUpdateIndexes(operation int, key string, indexes map[string]string) <-chan Indexer {
	ekey, err := NewKey(key)
	if err != nil {
		panic(err)
	}
	defer ekey.Free()
	responseCh := make(chan Indexer, defaultVOLUME)

	var cindexes []*C.char
	var cdatas []C.struct_go_data_pointer

	for index, data := range indexes {
		cindex := C.CString(index) // free this
		defer C.free(unsafe.Pointer(cindex))
		cindexes = append(cindexes, cindex)

		cdata := C.new_data_pointer(
			C.CString(data), // freed by ellipics::data_pointer in std::vector ???
			C.int(len(data)),
		)
		cdatas = append(cdatas, cdata)
	}

	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func() {
		//It's never called. For the future.
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &indexResult{err: err}
		}
		close(responseCh)

		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	// TODO: Reimplement this with pointer on functions
	switch operation {
	case indexesSet:
		C.session_set_indexes(s.session, C.context_t(onResultContext), C.context_t(onFinishContext),
			ekey.key,
			(**C.char)(&cindexes[0]),
			(*C.struct_go_data_pointer)(&cdatas[0]),
			C.uint64_t(len(cindexes)))

	case indexesUpdate:
		C.session_update_indexes(s.session, C.context_t(onResultContext), C.context_t(onFinishContext),
			ekey.key,
			(**C.char)(&cindexes[0]),
			(*C.struct_go_data_pointer)(&cdatas[0]),
			C.uint64_t(len(cindexes)))
	}
	return responseCh
}

//SetIndexes sets indexes for a given key.
func (s *Session) SetIndexes(key string, indexes map[string]string) <-chan Indexer {
	return s.setOrUpdateIndexes(indexesSet, key, indexes)
}

//UpdateIndexes sets indexes for a given key.
func (s *Session) UpdateIndexes(key string, indexes map[string]string) <-chan Indexer {
	return s.setOrUpdateIndexes(indexesUpdate, key, indexes)
}

//ListIndexes gets list of all indxes, which are associated with key.
func (s *Session) ListIndexes(key string) <-chan IndexEntry {
	responseCh := make(chan IndexEntry, defaultVOLUME)
	ekey, err := NewKey(key)
	if err != nil {
		panic(err)
	}
	defer ekey.Free()

	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func(indexentry *IndexEntry) {
		responseCh <- *indexentry
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- IndexEntry{err: err}
		}
		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)
	C.session_list_indexes(s.session, C.context_t(onResultContext), C.context_t(onFinishContext), ekey.key)
	return responseCh
}

//RemoveIndexes removes indexes from a key.
func (s *Session) RemoveIndexes(key string, indexes []string) <-chan Indexer {
	ekey, err := NewKey(key)
	if err != nil {
		panic(err)
	}
	defer ekey.Free()
	responseCh := make(chan Indexer, defaultVOLUME)

	var cindexes []*C.char
	for _, index := range indexes {
		cindex := C.CString(index) // free this
		defer C.free(unsafe.Pointer(cindex))
		cindexes = append(cindexes, cindex)
	}

	onResultContext := NextContext()
	onFinishContext := NextContext()

	onResult := func() {
		//It's never called. For the future.
	}

	onFinish := func(err error) {
		if err != nil {
			responseCh <- &indexResult{err: err}
		}
		close(responseCh)
		Pool.Delete(onResultContext)
		Pool.Delete(onFinishContext)
	}

	Pool.Store(onResultContext, onResult)
	Pool.Store(onFinishContext, onFinish)

	C.session_remove_indexes(s.session,
		C.context_t(onResultContext), C.context_t(onFinishContext),
		ekey.key, (**C.char)(&cindexes[0]), C.uint64_t(len(cindexes)))
	return responseCh
}

func (s *Session) LookupBackend(key string, group_id uint32) (addr *DnetAddr, backend_id int32, err error) {
	var caddr *C.struct_dnet_addr = C.dnet_addr_alloc()
	defer C.dnet_addr_free(caddr)
	var cbackend_id C.int

	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))

	addr = nil
	backend_id = -1

	cerr := C.session_lookup_addr(s.session, ckey, C.int(len(key)), C.int(group_id), caddr, &cbackend_id)
	if cerr < 0 {
		err = &DnetError{
			Code:  int(cerr),
			Flags: 0,
			Message: fmt.Sprintf("could not lookup backend: key '%s', group: %d: %d",
				key, group_id, int(cerr)),
		}

		return
	}

	new_addr := NewDnetAddr(caddr)

	addr = &new_addr
	backend_id = int32(cbackend_id)

	return
}