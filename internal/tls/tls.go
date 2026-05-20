package tls

import (
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Reloader struct {
	certPath string
	keyPath  string

	mu   sync.RWMutex
	cert *tls.Certificate
}

func NewReloader(certPath, keyPath string) (*Reloader, error) {
	r := &Reloader{
		certPath: certPath,
		keyPath:  keyPath,
	}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return fmt.Errorf("failed to load key pair: %w", err)
	}

	r.mu.Lock()
	r.cert = &cert
	r.mu.Unlock()
	return nil
}

func (r *Reloader) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}

func (r *Reloader) Start(stopCh <-chan struct{}) error {
	logger := log.Log.WithName("tls-reloader")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the directory instead of files to handle atomic k8s secret updates
	err = watcher.Add("/etc/tls")
	if err != nil {
		logger.Error(err, "Failed to watch /etc/tls, TLS reloader may not work")
		// Don't fail completely so local dev without mounts can work
	}

	go func() {
		defer func() { _ = watcher.Close() }()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Any event on the mounted secret dir can trigger a reload
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					// Debounce slightly to allow both files to update
					time.Sleep(50 * time.Millisecond)
					if err := r.reload(); err != nil {
						logger.Error(err, "Failed to reload TLS certificate")
					} else {
						logger.Info("Reloaded TLS certificate")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Error(err, "TLS watcher error")
			case <-stopCh:
				return
			}
		}
	}()
	return nil
}
