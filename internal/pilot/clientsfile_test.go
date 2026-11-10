package pilot

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kidcarmi/nac/internal/domain"
)

// interleavingStore forces the race from review: the first ListSwitches call
// holds its (older) snapshot until a second call has started, or 300 ms pass.
// Only ListSwitches is implemented; WriteClientsFile calls nothing else.
type interleavingStore struct {
	Store
	mu       sync.Mutex
	calls    int
	secondIn chan struct{}
}

func (f *interleavingStore) ListSwitches(context.Context) ([]domain.Switch, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	a := domain.Switch{Name: "a", RadiusSourceIP: "10.0.0.1", RadiusSecret: "secret-aaaaaaaaaaaa"}
	b := domain.Switch{Name: "b", RadiusSourceIP: "10.0.0.2", RadiusSecret: "secret-bbbbbbbbbbbb"}
	if n == 1 {
		select { // older snapshot, returned late
		case <-f.secondIn:
		case <-time.After(300 * time.Millisecond):
		}
		return []domain.Switch{a}, nil
	}
	close(f.secondIn)
	return []domain.Switch{a, b}, nil
}

func TestWriteClientsFile_NoStaleSnapshotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.conf")
	st := &interleavingStore{secondIn: make(chan struct{})}
	s := New(st, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{ClientsFile: path})

	first := make(chan struct{})
	go func() { defer close(first); s.WriteClientsFile(context.Background()) }()
	time.Sleep(50 * time.Millisecond) // let the first call take its snapshot
	s.WriteClientsFile(context.Background())
	<-first

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ipaddr = 10.0.0.2") {
		t.Fatalf("the newer switch list was overwritten by an older snapshot:\n%s", b)
	}
}
