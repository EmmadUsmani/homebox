package state

import (
	"reflect"
	"testing"
	"time"
)

func TestDoctorResultRoundTrip(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	box := Box{Name: "test", CreatedAt: time.Now().UTC()}
	if err := store.Save(box); err != nil {
		t.Fatal(err)
	}
	if result, err := store.LoadDoctorResult(box.Name); result != nil || err != nil {
		t.Fatalf("result before doctor: %+v, %v", result, err)
	}
	want := DoctorResult{CheckedAt: time.Now().UTC(), Passed: 5, Failed: 1, Skipped: 2, FailingChecks: []string{"dns"}}
	if err := store.SaveDoctorResult(box, want); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDoctorResult(box, DoctorResult{CheckedAt: want.CheckedAt.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadDoctorResult(box.Name)
	if err != nil || !reflect.DeepEqual(got, &want) {
		t.Fatalf("result = %+v, error = %v, want %+v", got, err, want)
	}
	replacement := box
	replacement.CreatedAt = replacement.CreatedAt.Add(time.Second)
	if err := store.Save(replacement); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDoctorResult(box, want); err == nil {
		t.Fatal("accepted a doctor result for a replaced box")
	}
}
