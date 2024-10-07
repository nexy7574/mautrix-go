// Copyright (c) 2024 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package hicli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/hicli/database"
	"maunium.net/go/mautrix/id"
)

func (h *HiClient) handleJSONCommand(ctx context.Context, req *JSONCommand) (any, error) {
	switch req.Command {
	case "get_state":
		return h.State(), nil
	case "cancel":
		return unmarshalAndCall(req.Data, func(params *cancelRequestParams) (bool, error) {
			h.jsonRequestsLock.Lock()
			cancelTarget, ok := h.jsonRequests[params.RequestID]
			h.jsonRequestsLock.Unlock()
			if ok {
				return false, nil
			}
			if params.Reason == "" {
				cancelTarget(nil)
			} else {
				cancelTarget(errors.New(params.Reason))
			}
			return true, nil
		})
	case "send_message":
		return unmarshalAndCall(req.Data, func(params *sendParams) (*database.Event, error) {
			return h.Send(ctx, params.RoomID, params.EventType, params.Content)
		})
	case "get_event":
		return unmarshalAndCall(req.Data, func(params *getEventParams) (*database.Event, error) {
			return h.GetEvent(ctx, params.RoomID, params.EventID)
		})
	case "get_events_by_rowids":
		return unmarshalAndCall(req.Data, func(params *getEventsByRowIDsParams) ([]*database.Event, error) {
			return h.GetEventsByRowIDs(ctx, params.RowIDs)
		})
	case "paginate":
		return unmarshalAndCall(req.Data, func(params *paginateParams) ([]*database.Event, error) {
			return h.Paginate(ctx, params.RoomID, params.MaxTimelineID, params.Limit)
		})
	case "paginate_server":
		return unmarshalAndCall(req.Data, func(params *paginateParams) ([]*database.Event, error) {
			return h.PaginateServer(ctx, params.RoomID, params.Limit)
		})
	case "ensure_group_session_shared":
		return unmarshalAndCall(req.Data, func(params *ensureGroupSessionSharedParams) (bool, error) {
			return true, h.EnsureGroupSessionShared(ctx, params.RoomID)
		})
	case "login":
		return unmarshalAndCall(req.Data, func(params *loginParams) (bool, error) {
			return true, h.LoginPassword(ctx, params.HomeserverURL, params.Username, params.Password)
		})
	case "verify":
		return unmarshalAndCall(req.Data, func(params *verifyParams) (bool, error) {
			return true, h.VerifyWithRecoveryKey(ctx, params.RecoveryKey)
		})
	case "discover_homeserver":
		return unmarshalAndCall(req.Data, func(params *discoverHomeserverParams) (*mautrix.ClientWellKnown, error) {
			_, homeserver, err := params.UserID.Parse()
			if err != nil {
				return nil, err
			}
			return mautrix.DiscoverClientAPI(ctx, homeserver)
		})
	default:
		return nil, fmt.Errorf("unknown command %q", req.Command)
	}
}

func unmarshalAndCall[T, O any](data json.RawMessage, fn func(*T) (O, error)) (output O, err error) {
	var input T
	err = json.Unmarshal(data, &input)
	if err != nil {
		return
	}
	return fn(&input)
}

type cancelRequestParams struct {
	RequestID int64  `json:"request_id"`
	Reason    string `json:"reason"`
}

type sendParams struct {
	RoomID    id.RoomID       `json:"room_id"`
	EventType event.Type      `json:"type"`
	Content   json.RawMessage `json:"content"`
}

type getEventParams struct {
	RoomID  id.RoomID  `json:"room_id"`
	EventID id.EventID `json:"event_id"`
}

type getEventsByRowIDsParams struct {
	RowIDs []database.EventRowID `json:"row_ids"`
}

type ensureGroupSessionSharedParams struct {
	RoomID id.RoomID `json:"room_id"`
}

type loginParams struct {
	HomeserverURL string `json:"homeserver_url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

type verifyParams struct {
	RecoveryKey string `json:"recovery_key"`
}

type discoverHomeserverParams struct {
	UserID id.UserID `json:"user_id"`
}

type paginateParams struct {
	RoomID        id.RoomID              `json:"room_id"`
	MaxTimelineID database.TimelineRowID `json:"max_timeline_id"`
	Limit         int                    `json:"limit"`
}