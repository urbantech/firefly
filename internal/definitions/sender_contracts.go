// Copyright © 2023 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
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

package definitions

import (
	"context"
	"errors"

	"github.com/hyperledger/firefly-common/pkg/fftypes"
	"github.com/hyperledger/firefly-common/pkg/i18n"
	"github.com/hyperledger/firefly/internal/coremsgs"
	"github.com/hyperledger/firefly/pkg/core"
	"github.com/hyperledger/firefly/pkg/database"
)

func (ds *definitionSender) DefineFFI(ctx context.Context, ffi *fftypes.FFI, waitConfirm bool) error {
	ffi.ID = fftypes.NewUUID()
	for _, method := range ffi.Methods {
		method.ID = fftypes.NewUUID()
	}
	for _, event := range ffi.Events {
		event.ID = fftypes.NewUUID()
	}
	for _, errorDef := range ffi.Errors {
		errorDef.ID = fftypes.NewUUID()
	}

	existing, err := ds.contracts.GetFFI(ctx, ffi.Name, ffi.NetworkName, ffi.Version)
	if existing != nil && err == nil {
		return i18n.NewError(ctx, coremsgs.MsgContractInterfaceExists, ffi.Namespace, ffi.Name, ffi.Version)
	}

	if ffi.Published {
		if !ds.multiparty {
			return i18n.NewError(ctx, coremsgs.MsgActionNotSupported)
		}
		_, err := ds.getFFISender(ctx, ffi).send(ctx, waitConfirm)
		return err
	}

	ffi.NetworkName = ""

	return fakeBatch(ctx, func(ctx context.Context, state *core.BatchState) (HandlerResult, error) {
		hr, err := ds.handler.handleFFIDefinition(ctx, state, ffi, nil)
		if err != nil {
			if innerErr := errors.Unwrap(err); innerErr != nil {
				return hr, innerErr
			}
		}
		return hr, err
	})
}

func (ds *definitionSender) getFFISender(ctx context.Context, ffi *fftypes.FFI) *sendWrapper {
	if err := ds.contracts.ResolveFFI(ctx, ffi); err != nil {
		return wrapSendError(err)
	}

	// Prepare the FFI definition to be serialized for broadcast
	localName := ffi.Name
	ffi.Name = ""
	ffi.Namespace = ""
	ffi.Published = true
	if ffi.NetworkName == "" {
		ffi.NetworkName = localName
	}

	sender := ds.getSenderDefault(ctx, ffi, core.SystemTagDefineFFI)
	if sender.message != nil {
		ffi.Message = sender.message.Header.ID
	}

	ffi.Name = localName
	ffi.Namespace = ds.namespace
	return sender
}

func (ds *definitionSender) PublishFFI(ctx context.Context, name, version, networkName string, waitConfirm bool) (ffi *fftypes.FFI, err error) {
	if !ds.multiparty {
		return nil, i18n.NewError(ctx, coremsgs.MsgActionNotSupported)
	}

	var sender *sendWrapper
	err = ds.database.RunAsGroup(ctx, func(ctx context.Context) error {
		if ffi, err = ds.contracts.GetFFI(ctx, name, "", version); err != nil {
			return err
		}
		if networkName != "" {
			ffi.NetworkName = networkName
		}

		sender = ds.getFFISender(ctx, ffi)
		if sender.err != nil {
			return sender.err
		}
		if !waitConfirm {
			if err = sender.sender.Prepare(ctx); err != nil {
				return err
			}
			if err = ds.database.UpsertFFI(ctx, ffi, database.UpsertOptimizationExisting); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	_, err = sender.send(ctx, waitConfirm)
	return ffi, err
}

func (ds *definitionSender) DefineContractAPI(ctx context.Context, httpServerURL string, api *core.ContractAPI, waitConfirm bool) error {
	if api.ID == nil {
		api.ID = fftypes.NewUUID()
	}

	if ds.multiparty {
		if err := ds.contracts.ResolveContractAPI(ctx, httpServerURL, api); err != nil {
			return err
		}

		api.Namespace = ""
		msg, err := ds.getSenderDefault(ctx, api, core.SystemTagDefineContractAPI).send(ctx, waitConfirm)
		if msg != nil {
			api.Message = msg.Header.ID
		}
		api.Namespace = ds.namespace
		return err
	}

	return fakeBatch(ctx, func(ctx context.Context, state *core.BatchState) (HandlerResult, error) {
		return ds.handler.handleContractAPIDefinition(ctx, state, httpServerURL, api, nil)
	})
}
