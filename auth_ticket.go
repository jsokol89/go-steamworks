// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The go-steamworks Authors

package steamworks

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	flatAPI_RegisterCallback   = "SteamAPI_RegisterCallback"
	flatAPI_UnregisterCallback = "SteamAPI_UnregisterCallback"

	// k_iSteamUserCallbacks (steam_api_internal.h).
	k_iSteamUserCallbacks = 100
	// GetTicketForWebApiResponse_t::k_iCallback (isteamuser.h).
	k_iCallback_GetTicketForWebApiResponse = k_iSteamUserCallbacks + 68
	// GetTicketForWebApiResponse_t::k_nCubTicketMaxLength.
	webApiTicketMaxLength = 2560
)

var (
	ptrAPI_RegisterCallback   func(uintptr, int32)
	ptrAPI_UnregisterCallback func(uintptr)
)

// webTicketPayload mirrors GetTicketForWebApiResponse_t (packed(4)):
// HAuthTicket m_hAuthTicket; EResult m_eResult; int m_cubTicket;
// uint8 m_rgubTicket[k_nCubTicketMaxLength].
type webTicketPayload struct {
	hAuthTicket HAuthTicket
	eResult     EResult
	cubTicket   int32
	rgubTicket  [webApiTicketMaxLength]byte
}

// ccbVtable is the C++ vtable of a CCallbackBase: the three virtual
// functions in declaration order (CCallbackBase has no virtual destructor).
type ccbVtable struct {
	run             uintptr
	runCallResult   uintptr
	getCallbackSize uintptr
}

// ccbObject mirrors CCallbackBase: vtable pointer, m_nCallbackFlags,
// m_iCallback (the SDK fills the latter in SteamAPI_RegisterCallback).
type ccbObject struct {
	vtable    unsafe.Pointer
	flags     uint8
	_         [3]byte
	iCallback int32
}

var (
	webTicketCallbackOnce   sync.Once
	webTicketCallbackVtable *ccbVtable
	webTicketCallbackObject *ccbObject
)

var webTicketWaiters = struct {
	sync.Mutex
	m map[HAuthTicket]chan GetTicketForWebApiResponse
}{m: make(map[HAuthTicket]chan GetTicketForWebApiResponse)}

// ensureWebTicketCallback registers a CCallbackBase for
// GetTicketForWebApiResponse_t. The SDK is a C++ library, so the callback
// object is emulated with a Go-built vtable whose slots point at purego
// callbacks. Must only be called after Init.
func ensureWebTicketCallback() {
	webTicketCallbackOnce.Do(func() {
		vtable := &ccbVtable{
			run: purego.NewCallback(func(pvParam unsafe.Pointer) {
				deliverWebTicketCallback(pvParam)
			}),
			runCallResult: purego.NewCallback(func(pvParam unsafe.Pointer, bIOFailure bool, hSteamAPICall uint32) {
				// Not used: GetTicketForWebApiResponse_t is a plain callback,
				// not a call result.
			}),
			getCallbackSize: purego.NewCallback(func() int32 {
				return int32(unsafe.Sizeof(webTicketPayload{}))
			}),
		}
		object := &ccbObject{vtable: unsafe.Pointer(vtable)}
		ptrAPI_RegisterCallback(uintptr(unsafe.Pointer(object)), int32(k_iCallback_GetTicketForWebApiResponse))
		webTicketCallbackVtable = vtable
		webTicketCallbackObject = object
	})
}

func registerWebTicketWaiter(h HAuthTicket) chan GetTicketForWebApiResponse {
	ch := make(chan GetTicketForWebApiResponse, 1)
	webTicketWaiters.Lock()
	webTicketWaiters.m[h] = ch
	webTicketWaiters.Unlock()
	return ch
}

func unregisterWebTicketWaiter(h HAuthTicket) {
	webTicketWaiters.Lock()
	delete(webTicketWaiters.m, h)
	webTicketWaiters.Unlock()
}

func deliverWebTicket(resp GetTicketForWebApiResponse) {
	webTicketWaiters.Lock()
	ch, ok := webTicketWaiters.m[resp.Handle]
	webTicketWaiters.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

func deliverWebTicketCallback(pvParam unsafe.Pointer) {
	p := (*webTicketPayload)(pvParam)
	n := int(p.cubTicket)
	if n < 0 {
		n = 0
	}
	if n > len(p.rgubTicket) {
		n = len(p.rgubTicket)
	}
	ticket := make([]byte, n)
	copy(ticket, p.rgubTicket[:n])
	deliverWebTicket(GetTicketForWebApiResponse{
		Handle: p.hAuthTicket,
		Result: p.eResult,
		Ticket: ticket,
	})
}

// WebApiAuthTicket requests an auth ticket for the Steam Web API
// (validated server-side with ISteamUserAuth/AuthenticateUserTicket) and
// blocks until the ticket arrives or the timeout elapses. Init must have
// succeeded and RunCallbacks must be pumped from another goroutine (the
// callback is delivered on the pumping goroutine); do not call this from
// the pumping goroutine itself. A non-positive timeout defaults to 10 s.
// The returned ticket is a credential: do not log it.
func WebApiAuthTicket(identity string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ensureWebTicketCallback()
	user := SteamUser()
	handle := user.GetAuthTicketForWebApi(identity)
	if handle == HAuthTicketInvalid {
		return "", errors.New("steamworks: GetAuthTicketForWebApi returned an invalid ticket handle")
	}
	ch := registerWebTicketWaiter(handle)
	defer unregisterWebTicketWaiter(handle)
	defer user.CancelAuthTicket(handle)
	select {
	case resp := <-ch:
		if resp.Result != EResultOK {
			return "", fmt.Errorf("steamworks: GetTicketForWebApiResponse: %v", resp.Result)
		}
		return string(resp.Ticket), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("steamworks: timed out after %v waiting for the Web API auth ticket", timeout)
	}
}
