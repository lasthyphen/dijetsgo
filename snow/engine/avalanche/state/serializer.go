// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state manages the meta-data required by consensus for an avalanche
// dag.
package state

import (
	"errors"

	"github.com/lasthyphen/dijetsgo/cache"
	"github.com/lasthyphen/dijetsgo/database"
	"github.com/lasthyphen/dijetsgo/database/versiondb"
	"github.com/lasthyphen/dijetsgo/ids"
	"github.com/lasthyphen/dijetsgo/snow"
	"github.com/lasthyphen/dijetsgo/snow/choices"
	"github.com/lasthyphen/dijetsgo/snow/consensus/avalanche"
	"github.com/lasthyphen/dijetsgo/snow/consensus/snowstorm"
	"github.com/lasthyphen/dijetsgo/snow/engine/avalanche/vertex"
	"github.com/lasthyphen/dijetsgo/utils/math"
)

const (
	dbCacheSize = 10000
	idCacheSize = 1000
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errWrongChainID  = errors.New("wrong ChainID in vertex")
)

var _ vertex.Manager = &Serializer{}

// Serializer manages the state of multiple vertices
type Serializer struct {
	ctx   *snow.Context
	vm    vertex.DAGVM
	state *prefixedState
	db    *versiondb.Database
	edge  ids.Set
}

func (s *Serializer) Initialize(ctx *snow.Context, vm vertex.DAGVM, db database.Database) {
	s.ctx = ctx
	s.vm = vm

	vdb := versiondb.New(db)
	dbCache := &cache.LRU{Size: dbCacheSize}
	rawState := &state{
		serializer: s,
		dbCache:    dbCache,
		db:         vdb,
	}
	s.state = newPrefixedState(rawState, idCacheSize)
	s.db = vdb

	s.edge.Add(s.state.Edge()...)
}

func (s *Serializer) ParseVtx(b []byte) (avalanche.Vertex, error) {
	return newUniqueVertex(s, b)
}

func (s *Serializer) BuildVtx(
	parentIDs []ids.ID,
	txs []snowstorm.Tx,
) (avalanche.Vertex, error) {
	height := uint64(0)
	for _, parentID := range parentIDs {
		parent, err := s.getVertex(parentID)
		if err != nil {
			return nil, err
		}
		parentHeight := parent.v.vtx.Height()
		childHeight, err := math.Add64(parentHeight, 1)
		if err != nil {
			return nil, err
		}
		height = math.Max64(height, childHeight)
	}

	txBytes := make([][]byte, len(txs))
	for i, tx := range txs {
		txBytes[i] = tx.Bytes()
	}

	vtx, err := vertex.Build(
		s.ctx.ChainID,
		height,
		parentIDs,
		txBytes,
	)
	if err != nil {
		return nil, err
	}

	uVtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtx.ID(),
	}
	// setVertex handles the case where this vertex already exists even
	// though we just made it
	return uVtx, uVtx.setVertex(vtx)
}

func (s *Serializer) BuildStopVtx(parentIDs []ids.ID) (avalanche.Vertex, error) {
	panic("not implemented")
}

func (s *Serializer) GetVtx(vtxID ids.ID) (avalanche.Vertex, error) { return s.getVertex(vtxID) }

func (s *Serializer) Edge() []ids.ID { return s.edge.List() }

func (s *Serializer) parseVertex(b []byte) (vertex.StatelessVertex, error) {
	vtx, err := vertex.Parse(b)
	if err != nil {
		return nil, err
	}
	if vtx.ChainID() != s.ctx.ChainID {
		return nil, errWrongChainID
	}
	return vtx, nil
}

func (s *Serializer) getVertex(vtxID ids.ID) (*uniqueVertex, error) {
	vtx := &uniqueVertex{
		serializer: s,
		vtxID:      vtxID,
	}
	if vtx.Status() == choices.Unknown {
		return nil, errUnknownVertex
	}
	return vtx, nil
}
