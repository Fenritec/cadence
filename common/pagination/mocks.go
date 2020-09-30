// The MIT License (MIT)
//
// Copyright (c) 2017-2020 Uber Technologies Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

// Code generated by MockGen. DO NOT EDIT.
// Source: interface.go

// Package pagination is a generated GoMock package.
package pagination

import (
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockEntity is a mock of Entity interface
type MockEntity struct {
	ctrl     *gomock.Controller
	recorder *MockEntityMockRecorder
}

// MockEntityMockRecorder is the mock recorder for MockEntity
type MockEntityMockRecorder struct {
	mock *MockEntity
}

// NewMockEntity creates a new mock instance
func NewMockEntity(ctrl *gomock.Controller) *MockEntity {
	mock := &MockEntity{ctrl: ctrl}
	mock.recorder = &MockEntityMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockEntity) EXPECT() *MockEntityMockRecorder {
	return m.recorder
}

// MockPageToken is a mock of PageToken interface
type MockPageToken struct {
	ctrl     *gomock.Controller
	recorder *MockPageTokenMockRecorder
}

// MockPageTokenMockRecorder is the mock recorder for MockPageToken
type MockPageTokenMockRecorder struct {
	mock *MockPageToken
}

// NewMockPageToken creates a new mock instance
func NewMockPageToken(ctrl *gomock.Controller) *MockPageToken {
	mock := &MockPageToken{ctrl: ctrl}
	mock.recorder = &MockPageTokenMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockPageToken) EXPECT() *MockPageTokenMockRecorder {
	return m.recorder
}

// MockIterator is a mock of Iterator interface
type MockIterator struct {
	ctrl     *gomock.Controller
	recorder *MockIteratorMockRecorder
}

// MockIteratorMockRecorder is the mock recorder for MockIterator
type MockIteratorMockRecorder struct {
	mock *MockIterator
}

// NewMockIterator creates a new mock instance
func NewMockIterator(ctrl *gomock.Controller) *MockIterator {
	mock := &MockIterator{ctrl: ctrl}
	mock.recorder = &MockIteratorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockIterator) EXPECT() *MockIteratorMockRecorder {
	return m.recorder
}

// Next mocks base method
func (m *MockIterator) Next() (Entity, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Next")
	ret0, _ := ret[0].(Entity)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Next indicates an expected call of Next
func (mr *MockIteratorMockRecorder) Next() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Next", reflect.TypeOf((*MockIterator)(nil).Next))
}

// HasNext mocks base method
func (m *MockIterator) HasNext() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasNext")
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasNext indicates an expected call of HasNext
func (mr *MockIteratorMockRecorder) HasNext() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasNext", reflect.TypeOf((*MockIterator)(nil).HasNext))
}

// MockWriter is a mock of Writer interface
type MockWriter struct {
	ctrl     *gomock.Controller
	recorder *MockWriterMockRecorder
}

// MockWriterMockRecorder is the mock recorder for MockWriter
type MockWriterMockRecorder struct {
	mock *MockWriter
}

// NewMockWriter creates a new mock instance
func NewMockWriter(ctrl *gomock.Controller) *MockWriter {
	mock := &MockWriter{ctrl: ctrl}
	mock.recorder = &MockWriterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockWriter) EXPECT() *MockWriterMockRecorder {
	return m.recorder
}

// Add mocks base method
func (m *MockWriter) Add(arg0 Entity) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Add", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Add indicates an expected call of Add
func (mr *MockWriterMockRecorder) Add(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Add", reflect.TypeOf((*MockWriter)(nil).Add), arg0)
}

// Flush mocks base method
func (m *MockWriter) Flush() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Flush")
	ret0, _ := ret[0].(error)
	return ret0
}

// Flush indicates an expected call of Flush
func (mr *MockWriterMockRecorder) Flush() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Flush", reflect.TypeOf((*MockWriter)(nil).Flush))
}

// FlushIfNotEmpty mocks base method
func (m *MockWriter) FlushIfNotEmpty() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FlushIfNotEmpty")
	ret0, _ := ret[0].(error)
	return ret0
}

// FlushIfNotEmpty indicates an expected call of FlushIfNotEmpty
func (mr *MockWriterMockRecorder) FlushIfNotEmpty() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FlushIfNotEmpty", reflect.TypeOf((*MockWriter)(nil).FlushIfNotEmpty))
}

// FlushedPages mocks base method
func (m *MockWriter) FlushedPages() []PageToken {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FlushedPages")
	ret0, _ := ret[0].([]PageToken)
	return ret0
}

// FlushedPages indicates an expected call of FlushedPages
func (mr *MockWriterMockRecorder) FlushedPages() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FlushedPages", reflect.TypeOf((*MockWriter)(nil).FlushedPages))
}

// FirstFlushedPage mocks base method
func (m *MockWriter) FirstFlushedPage() PageToken {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FirstFlushedPage")
	ret0, _ := ret[0].(PageToken)
	return ret0
}

// FirstFlushedPage indicates an expected call of FirstFlushedPage
func (mr *MockWriterMockRecorder) FirstFlushedPage() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FirstFlushedPage", reflect.TypeOf((*MockWriter)(nil).FirstFlushedPage))
}

// LastFlushedPage mocks base method
func (m *MockWriter) LastFlushedPage() PageToken {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LastFlushedPage")
	ret0, _ := ret[0].(PageToken)
	return ret0
}

// LastFlushedPage indicates an expected call of LastFlushedPage
func (mr *MockWriterMockRecorder) LastFlushedPage() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LastFlushedPage", reflect.TypeOf((*MockWriter)(nil).LastFlushedPage))
}
