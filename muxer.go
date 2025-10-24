package tunnel

import (
	"context"
	"errors"
	"net"
	"sync/atomic"

	"github.com/xtaci/smux"
)

type Muxer interface {
	// OpenConn 开启一个虚拟子流。
	OpenConn(ctx context.Context) (net.Conn, error)

	// IsClosed 判断底层连接是否关闭。
	IsClosed() bool
}

type safeMuxer struct {
	ptr atomic.Pointer[smux.Session]
}

func (sm *safeMuxer) OpenConn(context.Context) (net.Conn, error) {
	sess := sm.ptr.Load()
	if sess == nil {
		return nil, errors.New("session uninitialized")
	}

	if stm, err := sess.OpenStream(); err != nil {
		return nil, err
	} else {
		return stm, nil
	}
}

func (sm *safeMuxer) IsClosed() bool {
	if sess := sm.load(); sess != nil {
		return sess.IsClosed()
	}

	return false
}

func (sm *safeMuxer) store(sess *smux.Session) {
	if sess == nil {
		panic("nil session is not allowed")
	}

	sm.ptr.Store(sess)
}

func (sm *safeMuxer) load() *smux.Session {
	if sess := sm.ptr.Load(); sess != nil {
		return sess
	}

	panic("session uninitialized")
}

type muxerListener struct {
	mux *safeMuxer
}

func (m *muxerListener) Accept() (net.Conn, error) {
	sess := m.mux.load()
	if stm, err := sess.AcceptStream(); err != nil {
		return nil, err
	} else {
		return stm, nil
	}
}

func (m *muxerListener) Close() error {
	return m.mux.load().Close()
}

func (m *muxerListener) Addr() net.Addr {
	return m.mux.load().LocalAddr()
}
