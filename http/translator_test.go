package http_test

import (
	"context"
	"io"
	"io/ioutil"
	"testing"
	"time"

	"github.com/pilosa/pilosa"
	"github.com/pilosa/pilosa/http"
	"github.com/pilosa/pilosa/mock"
	"github.com/pilosa/pilosa/server"
	"github.com/pilosa/pilosa/test"
)

func TestTranslateStore_Reader(t *testing.T) {
	t.Skip() // Until test.NewServer() works

	// Ensure client can connect and stream the translate store data.
	t.Run("OK", func(t *testing.T) {
		t.Run("ServerDisconnect", func(t *testing.T) {
			var mrc mock.ReadCloser
			var readN int
			mrc.ReadFunc = func(p []byte) (int, error) {
				readN++
				switch readN {
				case 1:
					copy(p, []byte("foo"))
					return 3, nil
				case 2:
					copy(p, []byte("barbaz"))
					return 6, nil
				case 3:
					return 0, io.EOF
				default:
					t.Fatal("unexpected read")
					return 0, nil
				}
			}
			var closeInvoked bool
			mrc.CloseFunc = func() error {
				closeInvoked = true
				return nil
			}

			// Setup handler on test server.
			var translateStore mock.TranslateStore
			translateStore.ReaderFunc = func(ctx context.Context, off int64) (io.ReadCloser, error) {
				if off != 100 {
					t.Fatalf("unexpected off: %d", off)
				}
				return &mrc, nil
			}

			opts := server.OptCommandServerOptions(pilosa.OptServerPrimaryTranslateStore(translateStore))
			main := test.MustRunMainWithCluster(t, 1, opts)[0]
			defer main.Close()

			// Connect to server and stream all available data.
			store := http.NewTranslateStore(main.Server.URI.String())

			rc, err := store.Reader(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			} else if data, err := ioutil.ReadAll(rc); err != nil {
				t.Fatal(err)
			} else if string(data) != `foobarbaz` {
				t.Fatalf("unexpected data: %q", data)
			} else if err := rc.Close(); err != nil {
				t.Fatal(err)
			}

			if !closeInvoked {
				t.Fatal("expected server close")
			}
		})

		// Ensure server closes store reader if client disconnects.
		t.Run("ClientDisconnect", func(t *testing.T) {
			// Setup mock so that Read() hangs.
			done := make(chan struct{})

			var mrc mock.ReadCloser
			mrc.ReadFunc = func(p []byte) (int, error) {
				<-done
				return 0, io.EOF
			}
			var closeInvoked bool
			mrc.CloseFunc = func() error {
				closeInvoked = true
				return nil
			}

			var translateStore mock.TranslateStore
			translateStore.ReaderFunc = func(ctx context.Context, off int64) (io.ReadCloser, error) {
				return &mrc, nil
			}

			opts := server.OptCommandServerOptions(pilosa.OptServerPrimaryTranslateStore(translateStore))
			main := test.MustRunMainWithCluster(t, 1, opts)[0]

			defer main.Close()
			defer close(done)

			// Connect to server and begin streaming.
			ctx, cancel := context.WithCancel(context.Background())
			store := http.NewTranslateStore(main.Server.URI.String())
			if _, err := store.Reader(ctx, 0); err != nil {
				t.Fatal(err)
			}

			// Cancel the context and check if server is closed.
			cancel()
			time.Sleep(100 * time.Millisecond)
			if !closeInvoked {
				t.Fatal("expected server-side close")
			}
		})
	})

	// Ensure client is notified if the server doesn't support streaming replication.
	t.Run("ErrNotImplemented", func(t *testing.T) {
		var translateStore mock.TranslateStore
		translateStore.ReaderFunc = func(ctx context.Context, off int64) (io.ReadCloser, error) {
			return nil, pilosa.ErrNotImplemented
		}

		opts := server.OptCommandServerOptions(pilosa.OptServerPrimaryTranslateStore(translateStore))
		main := test.MustRunMainWithCluster(t, 1, opts)[0]

		_, err := http.NewTranslateStore(main.Server.URI.String()).Reader(context.Background(), 0)
		if err != pilosa.ErrNotImplemented {
			t.Fatalf("unexpected error: %s", err)
		}
	})
}
