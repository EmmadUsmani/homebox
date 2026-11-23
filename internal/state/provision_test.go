package state

import (
	"testing"
	"time"
)

func TestProvisionRunRoundTrip(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	if _, ok, err := store.LoadProvisionRun("main"); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	started := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	want := ProvisionRun{PID: 42, StartedAt: started, Step: "Docker Engine", Log: "20261122T100000Z.log"}
	if err := store.SaveProvisionRun("main", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.LoadProvisionRun("main")
	if err != nil || !ok || got.PID != want.PID || !got.StartedAt.Equal(started) || got.Step != want.Step || got.Log != want.Log || got.FinishedAt != nil {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
	if err := store.SaveProvisionRun("../x", want); err == nil {
		t.Fatal("invalid name accepted")
	}
}
