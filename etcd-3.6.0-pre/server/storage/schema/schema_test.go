// Copyright 2021 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schema

import (
	"testing"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/stretchr/testify/assert"
	"go.etcd.io/etcd/api/v3/version"
	"go.etcd.io/etcd/raft/v3/raftpb"
	"go.etcd.io/etcd/server/v3/storage/backend"
	betesting "go.etcd.io/etcd/server/v3/storage/backend/testing"
	"go.uber.org/zap"
)

func TestUpdateStorageVersion(t *testing.T) {
	tcs := []struct {
		name             string
		version          string
		metaKeys         [][]byte
		expectVersion    *semver.Version
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name:             `Backend before 3.6 without "confState" should be rejected`,
			version:          "",
			expectVersion:    nil,
			expectError:      true,
			expectedErrorMsg: `cannot determine storage version: missing "confState" key`,
		},
		{
			name:             `Backend before 3.6 without "term" should be rejected`,
			version:          "",
			metaKeys:         [][]byte{MetaConfStateName},
			expectVersion:    nil,
			expectError:      true,
			expectedErrorMsg: `cannot determine storage version: missing "term" key`,
		},
		{
			name:          "Backend with 3.5 with all metadata keys should be upgraded to v3.6",
			version:       "",
			metaKeys:      [][]byte{MetaTermKeyName, MetaConfStateName},
			expectVersion: &semver.Version{Major: 3, Minor: 6},
		},
		{
			name:          "Backend in 3.6.0 should be skipped",
			version:       "3.6.0",
			metaKeys:      [][]byte{MetaTermKeyName, MetaConfStateName, MetaStorageVersionName},
			expectVersion: &semver.Version{Major: 3, Minor: 6},
		},
		{
			name:          "Backend with current version should be skipped",
			version:       version.Version,
			metaKeys:      [][]byte{MetaTermKeyName, MetaConfStateName, MetaStorageVersionName},
			expectVersion: &semver.Version{Major: 3, Minor: 6},
		},
		{
			name:          "Backend in 3.7.0 should be skipped",
			version:       "3.7.0",
			metaKeys:      [][]byte{MetaTermKeyName, MetaConfStateName, MetaStorageVersionName, []byte("future-key")},
			expectVersion: &semver.Version{Major: 3, Minor: 7},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			lg := zap.NewNop()
			be, tmpPath := betesting.NewTmpBackend(t, time.Microsecond, 10)
			tx := be.BatchTx()
			if tx == nil {
				t.Fatal("batch tx is nil")
			}
			tx.Lock()
			UnsafeCreateMetaBucket(tx)
			for _, k := range tc.metaKeys {
				switch string(k) {
				case string(MetaConfStateName):
					MustUnsafeSaveConfStateToBackend(lg, tx, &raftpb.ConfState{})
				case string(MetaTermKeyName):
					UnsafeUpdateConsistentIndex(tx, 1, 1, false)
				default:
					tx.UnsafePut(Meta, k, []byte{})
				}
			}
			if tc.version != "" {
				UnsafeSetStorageVersion(tx, semver.New(tc.version))
			}
			tx.Unlock()
			be.ForceCommit()
			be.Close()

			b := backend.NewDefaultBackend(tmpPath)
			defer b.Close()
			err := UpdateStorageSchema(lg, b.BatchTx())
			if (err != nil) != tc.expectError {
				t.Errorf("UpgradeStorage(...) = %+v, expected error: %v", err, tc.expectError)
			}
			if err != nil && err.Error() != tc.expectedErrorMsg {
				t.Errorf("UpgradeStorage(...) = %q, expected error message: %q", err, tc.expectedErrorMsg)
			}
			v := UnsafeReadStorageVersion(b.BatchTx())
			assert.Equal(t, tc.expectVersion, v)
		})
	}
}
