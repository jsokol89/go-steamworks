// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The go-steamworks Authors

package steamworks

import (
	"testing"
	"unsafe"
)

// TestWebTicketPayloadLayout pins the ABI assumption of the
// GetTicketForWebApiResponse_t callback: HAuthTicket + EResult + int +
// 2560 bytes, packed(4).
func TestWebTicketPayloadLayout(t *testing.T) {
	if got, want := int(unsafe.Sizeof(webTicketPayload{})), 2572; got != want {
		t.Fatalf("sizeof(webTicketPayload) = %d, want %d", got, want)
	}
	if off := int(unsafe.Offsetof(webTicketPayload{}.rgubTicket)); off != 12 {
		t.Fatalf("offset of rgubTicket = %d, want 12", off)
	}
}

// TestCCBObjectLayout pins the CCallbackBase object size: vtable pointer
// + flags byte + 3 padding + int32 iCallback.
func TestCCBObjectLayout(t *testing.T) {
	if got, want := int(unsafe.Sizeof(ccbObject{})), int(unsafe.Sizeof(uintptr(0)))+8; got != want {
		t.Fatalf("sizeof(ccbObject) = %d, want %d", got, want)
	}
}

func TestEResultString(t *testing.T) {
	for _, tc := range []struct {
		e    EResult
		want string
	}{
		{EResultOK, "OK"},
		{EResultFail, "Fail"},
		{EResultTimeout, "Timeout"},
		{EResult(999), "Unknown(999)"},
	} {
		if got := tc.e.String(); got != tc.want {
			t.Errorf("EResult(%d).String() = %q, want %q", int(tc.e), got, tc.want)
		}
	}
}
