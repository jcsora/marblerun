// Copyright (c) Edgeless Systems GmbH.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package core

import (
	"encoding/json"
	"testing"

	"github.com/edgelesssys/marblerun/coordinator/clientapi"
	"github.com/edgelesssys/marblerun/coordinator/manifest"
	"github.com/edgelesssys/marblerun/coordinator/quote"
	"github.com/edgelesssys/marblerun/coordinator/recovery"
	"github.com/edgelesssys/marblerun/coordinator/seal"
	"github.com/edgelesssys/marblerun/coordinator/state"
	"github.com/edgelesssys/marblerun/coordinator/store/stdstore"
	"github.com/edgelesssys/marblerun/test"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStoreWrapperMetrics(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	zapLogger, err := zap.NewDevelopment()
	require.NoError(err)
	defer zapLogger.Sync()
	validator := quote.NewMockValidator()
	issuer := quote.NewMockIssuer()
	sealer := &seal.MockSealer{}
	recovery := recovery.NewSinglePartyRecovery()

	//
	// Test unset restart and set manifest.
	//
	reg := prometheus.NewRegistry()
	fac := promauto.With(reg)
	c, _ := NewCore([]string{"localhost"}, validator, issuer, stdstore.New(sealer), recovery, zapLogger, &fac, nil)
	assert.Equal(1, promtest.CollectAndCount(c.metrics.coordinatorState))
	assert.Equal(float64(state.AcceptingManifest), promtest.ToFloat64(c.metrics.coordinatorState))

	clientAPI, err := clientapi.New(c.store, c.recovery, c, zapLogger)
	require.NoError(err)
	_, err = clientAPI.SetManifest([]byte(test.ManifestJSON))
	require.NoError(err)
	assert.Equal(1, promtest.CollectAndCount(c.metrics.coordinatorState))
	assert.Equal(float64(state.AcceptingMarbles), promtest.ToFloat64(c.metrics.coordinatorState))

	//
	// Test sealing and recovery.
	//
	reg = prometheus.NewRegistry()
	fac = promauto.With(reg)
	sealer.UnsealError = seal.ErrEncryptionKey
	c, err = NewCore([]string{"localhost"}, validator, issuer, stdstore.New(sealer), recovery, zapLogger, &fac, nil)
	sealer.UnsealError = nil
	require.NoError(err)
	assert.Equal(1, promtest.CollectAndCount(c.metrics.coordinatorState))
	assert.Equal(float64(state.Recovery), promtest.ToFloat64(c.metrics.coordinatorState))

	clientAPI, err = clientapi.New(c.store, c.recovery, c, zapLogger)
	require.NoError(err)

	key := make([]byte, 16)
	_, err = clientAPI.Recover(key)
	require.NoError(err)
	state, err := c.data.GetState()
	require.NoError(err)
	assert.Equal(1, promtest.CollectAndCount(c.metrics.coordinatorState))
	assert.Equal(float64(state), promtest.ToFloat64(c.metrics.coordinatorState))
}

func TestMarbleAPIMetrics(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// parse manifest
	var manifest manifest.Manifest
	require.NoError(json.Unmarshal([]byte(test.ManifestJSON), &manifest))

	// setup mock zaplogger which can be passed to Core
	zapLogger, err := zap.NewDevelopment()
	require.NoError(err)
	defer zapLogger.Sync()

	// create core
	validator := quote.NewMockValidator()
	issuer := quote.NewMockIssuer()
	sealer := &seal.MockSealer{}
	recovery := recovery.NewSinglePartyRecovery()
	promRegistry := prometheus.NewRegistry()
	promFactory := promauto.With(promRegistry)
	c, err := NewCore([]string{"localhost"}, validator, issuer, stdstore.New(sealer), recovery, zapLogger, &promFactory, nil)
	require.NoError(err)
	require.NotNil(c)

	metrics := c.metrics.marbleAPI
	assert.Equal(0, promtest.CollectAndCount(metrics.activation))
	assert.Equal(0, promtest.CollectAndCount(metrics.activationSuccess))

	spawner := marbleSpawner{
		assert:     assert,
		require:    require,
		issuer:     issuer,
		validator:  validator,
		manifest:   manifest,
		coreServer: c,
	}

	// try to activate first backend marble prematurely before manifest is set
	uuid := spawner.newMarble("backendFirst", "Azure", false)
	promtest.CollectAndCount(metrics.activation)
	promtest.CollectAndCount(metrics.activationSuccess)
	assert.Equal(float64(1), promtest.ToFloat64(metrics.activation.WithLabelValues("backendFirst", uuid)))
	assert.Equal(float64(0), promtest.ToFloat64(metrics.activationSuccess.WithLabelValues("backendFirst", uuid)))

	// set manifest
	clientAPI, err := clientapi.New(c.store, c.recovery, c, zapLogger)
	require.NoError(err)
	_, err = clientAPI.SetManifest([]byte(test.ManifestJSON))
	require.NoError(err)

	// activate first backend
	uuid = spawner.newMarble("backendFirst", "Azure", true)
	promtest.CollectAndCount(metrics.activation)
	promtest.CollectAndCount(metrics.activationSuccess)
	assert.Equal(float64(1), promtest.ToFloat64(metrics.activation.WithLabelValues("backendFirst", uuid)))
	assert.Equal(float64(1), promtest.ToFloat64(metrics.activationSuccess.WithLabelValues("backendFirst", uuid)))

	// try to activate another first backend
	uuid = spawner.newMarble("backendFirst", "Azure", false)
	promtest.CollectAndCount(metrics.activation)
	promtest.CollectAndCount(metrics.activationSuccess)
	assert.Equal(float64(1), promtest.ToFloat64(metrics.activation.WithLabelValues("backendFirst", uuid)))
	assert.Equal(float64(0), promtest.ToFloat64(metrics.activationSuccess.WithLabelValues("backendFirst", uuid)))
}
