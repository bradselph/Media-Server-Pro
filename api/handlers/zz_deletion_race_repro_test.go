package handlers

// SCRATCH REPRODUCTION TEST — verifies (or refutes) the claimed unsynchronized
// double-checked-locking race on h.deletionRequests in requireDeletionRepo
// (deletion_requests.go:23). Run with:
//
//	go test ./api/handlers/... -run TestRequireDeletionRepo_ConcurrentRace -race -count=5
//
// This injects a non-nil *gorm.DB into the unexported db field of
// *database.Module via unsafe reflection, so requireDeletionRepo's real
// lazy-init path executes without needing a live MySQL connection.

import (
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
)

func setUnexportedDBField(m *database.Module, db *gorm.DB) {
	v := reflect.ValueOf(m).Elem().FieldByName("db")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
}

func TestRequireDeletionRepo_ConcurrentRace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbMod := database.NewModule(nil)
	setUnexportedDBField(dbMod, &gorm.DB{})

	h := &Handler{
		database: dbMod,
		log:      logger.New("race-test"),
	}

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			ok := h.requireDeletionRepo(c)
			if !ok {
				t.Errorf("requireDeletionRepo returned false unexpectedly")
				return
			}
			// Mirror what the real handlers do immediately after
			// requireDeletionRepo returns true: use h.deletionRequests
			// without re-checking nil or holding the mutex.
			if h.deletionRequests == nil {
				t.Errorf("requireDeletionRepo returned true but h.deletionRequests is nil")
			}
		}()
	}
	wg.Wait()
}
