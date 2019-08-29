package sync

import (
	"sync"

	"github.com/icon-project/goloop/common/codec"
	"github.com/icon-project/goloop/common/db"
	"github.com/icon-project/goloop/common/log"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/service/state"
)

const (
	configSyncPriority = 3
	configExpiredTime  = 300 // in millisecond
)

const (
	receiveMsg = iota
	receiveTimeExpired
)

var c = codec.MP

type Syncer interface {
	ForceSync() *Result
	Finalize() error
}

type Manager struct {
	log     log.Logger
	pool    *peerPool
	server  *server
	client  *client
	db      db.Database
	syncing bool
	syncer  *syncer
	mutex   sync.Mutex
}

type Result struct {
	Wss            state.WorldSnapshot
	PatchReceipts  module.ReceiptList
	NormalReceipts module.ReceiptList
}

func (m *Manager) OnReceive(pi module.ProtocolInfo, b []byte,
	id module.PeerID) (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.log.Debugf("OnReceive pi(%s), id(%s), syncing(%t)\n", pi, id, m.syncing)
	p := m.pool.getPeer(id)
	if p == nil {
		m.log.Debugf("peer(%s) is not valid\n", id)
		return false, nil
	}
	switch pi {
	case protoHasNode, protoRequestNodeData:
		m.server.onReceive(pi, b, p)
	case protoResult, protoNodeData:
		if m.syncing {
			m.syncer.onReceive(pi, b, p)
		}
	}
	return false, nil
}

func (m *Manager) OnFailure(err error, pi module.ProtocolInfo, b []byte) {
	m.log.Debugf("Manager OnFailure err(%+v), pi(%s)\n", err, pi)
}

func (m *Manager) OnJoin(id module.PeerID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.log.Debugf("Manager OnJoin syncing(%t)\n", m.syncing)
	np := &peer{
		id:    id,
		reqID: 0,
	}
	m.pool.push(np)
	if m.syncing {
		m.syncer.onJoin(np)
	}
}

func (m *Manager) OnLeave(id module.PeerID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.log.Debugf("Manager OnLeave id(%s)\n", id)
	p := m.pool.getPeer(id)
	if p == nil {
		return
	}
	if m.syncing {
		m.syncer.onLeave(id)
	}
	m.pool.remove(id)
}

func (m *Manager) NewSyncer(ah, prh, nrh, vh []byte) Syncer {
	m.log.Debugf(
		"NewSyncer accountHash(%#x), prh(%#x), nrh(%#x), vlh(%#x)\n",
		ah, prh, nrh, vh)
	m.syncer = newSyncer(
		m.db, m.client, m.pool,
		ah, prh, nrh, vh, m.log,
		func(syncing bool) {
			m.mutex.Lock()
			m.syncing = syncing
			if syncing == false {
				m.syncer = nil
			}
			m.mutex.Unlock()
		})
	return m.syncer
}

func NewSyncManager(db db.Database, nm module.NetworkManager, logger log.Logger) *Manager {
	logger.Info("NewSyncManager\n")
	m := new(Manager)
	ph, err := nm.RegisterReactorForStreams(
		"statesync", m, protocol, configSyncPriority)
	if err != nil {
		log.Panicf("Failed to register reactor for stateSync\n")
		return nil
	}
	m.db = db
	m.log = logger

	server := newServer(db, ph, logger)
	m.server = server

	client := newClient(ph, logger)
	m.client = client
	m.pool = newPeerPool()
	return m
}
