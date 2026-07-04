package hub

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestDeviceStoreRegistersUpsertsAndListsExpoTokens(t *testing.T) {
	ctx := context.Background()
	store := newTestDeviceStore(t)
	store.now = func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) }

	first, err := store.Register(ctx, protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[first]",
		Platform:    "ios",
		AppID:       "dev.tethon.mobile",
		AppVersion:  "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.DeviceID != "phone-1" || first.PushService != "expo" || first.Platform != "ios" {
		t.Fatalf("register response = %+v", first)
	}

	store.now = func() time.Time { return time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC) }
	updated, err := store.Register(ctx, protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[second]",
		Platform:    "android",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated_at = %s, want after %s", updated.UpdatedAt, first.UpdatedAt)
	}

	tokens, err := store.ExpoPushTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "ExpoPushToken[second]" {
		t.Fatalf("tokens = %#v, want updated token", tokens)
	}
}

func TestDeviceStorePersistsAndUnregisters(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "hub.db")
	store, err := OpenDeviceStore(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(ctx, protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[token]",
		Platform:    "ios",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDeviceStore(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	tokens, err := reopened.ExpoPushTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "ExpoPushToken[token]" {
		t.Fatalf("tokens after reopen = %#v", tokens)
	}

	if err := reopened.Unregister(ctx, "phone-1"); err != nil {
		t.Fatal(err)
	}
	tokens, err = reopened.ExpoPushTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("tokens after unregister = %#v, want none", tokens)
	}
	if err := reopened.Unregister(ctx, "phone-1"); !errors.Is(err, errDeviceNotFound) {
		t.Fatalf("second unregister error = %v, want errDeviceNotFound", err)
	}
}

func TestDeviceStoreListsActiveExpoDevicesAndDisables(t *testing.T) {
	ctx := context.Background()
	store := newTestDeviceStore(t)
	if _, err := store.Register(ctx, protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[1]",
		Platform:    "ios",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(ctx, protocol.RegisterDeviceRequest{
		DeviceID:    "phone-2",
		PushService: "expo",
		PushToken:   "ExpoPushToken[2]",
		Platform:    "android",
	}); err != nil {
		t.Fatal(err)
	}

	devices, err := store.ActiveExpoDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %#v, want 2", devices)
	}
	if err := store.Disable(ctx, "phone-1"); err != nil {
		t.Fatal(err)
	}
	devices, err = store.ActiveExpoDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "phone-2" || devices[0].PushToken != "ExpoPushToken[2]" {
		t.Fatalf("devices after disable = %#v", devices)
	}
}

func TestValidateDevice(t *testing.T) {
	base := protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[token]",
		Platform:    "ios",
	}
	tests := []struct {
		name string
		req  protocol.RegisterDeviceRequest
	}{
		{name: "missing device id", req: protocol.RegisterDeviceRequest{PushService: "expo", PushToken: "token", Platform: "ios"}},
		{name: "slash device id", req: protocol.RegisterDeviceRequest{DeviceID: "bad/id", PushService: "expo", PushToken: "token", Platform: "ios"}},
		{name: "wrong service", req: protocol.RegisterDeviceRequest{DeviceID: "phone-1", PushService: "apns", PushToken: "token", Platform: "ios"}},
		{name: "missing token", req: protocol.RegisterDeviceRequest{DeviceID: "phone-1", PushService: "expo", Platform: "ios"}},
		{name: "wrong platform", req: protocol.RegisterDeviceRequest{DeviceID: "phone-1", PushService: "expo", PushToken: "token", Platform: "web"}},
	}
	if err := validateDevice(base); err != nil {
		t.Fatalf("valid request failed: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDevice(test.req); err == nil {
				t.Fatal("validateDevice succeeded, want error")
			}
		})
	}
}

func newTestDeviceStore(t *testing.T) *DeviceStore {
	t.Helper()
	store, err := OpenDeviceStore(context.Background(), filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return store
}
