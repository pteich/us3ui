package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/pteich/us3ui/storage"
)

func TestTransferReservationKeepsOriginalCancel(t *testing.T) {
	var f filesState
	ctx := f.beginTransfer("First")
	if ctx == nil || !f.xfer.active {
		t.Fatal("first transfer was not reserved")
	}
	if second := f.beginTransfer("Second"); second != nil {
		t.Fatal("overlapping transfer was accepted")
	}
	f.stopTransfer()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("Stop did not cancel the original transfer")
	}
	if f.beginTransfer("Too early") != nil {
		t.Fatal("accepted a transfer before the canceled worker finished")
	}
	f.finishTransfer()
	if next := f.beginTransfer("Next"); next == nil {
		t.Fatal("completed transfer did not release its reservation")
	}
	f.finishTransfer()
}

func TestDownloadConflictAppliesOnlyToCurrentTransfer(t *testing.T) {
	var f filesState
	f.beginTransfer("Download")
	first := make(chan storage.DownloadConflict, 1)
	f.conflict.request("a.txt", first)
	if !f.conflict.open {
		t.Fatal("first conflict did not open a dialog")
	}
	f.conflict.apply = true
	f.conflict.resolve(storage.KeepBothDownloads)
	if choice := <-first; choice != storage.KeepBothDownloads {
		t.Fatalf("first choice = %v", choice)
	}
	second := make(chan storage.DownloadConflict, 1)
	f.conflict.request("b.txt", second)
	select {
	case choice := <-second:
		if choice != storage.KeepBothDownloads || f.conflict.open {
			t.Fatal("remaining conflict did not reuse the choice")
		}
	default:
		t.Fatal("remaining conflict waited for another dialog")
	}
	f.finishTransfer()
	f.beginTransfer("New download")
	defer f.finishTransfer()
	f.conflict.request("c.txt", make(chan storage.DownloadConflict, 1))
	if !f.conflict.open {
		t.Fatal("conflict policy leaked into a new transfer")
	}
}
