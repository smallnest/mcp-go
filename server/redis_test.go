package server

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func TestRedisSessionStore_Basic(t *testing.T) {
	s := miniredis.RunT(t)

	store := NewRedisSessionStore(s.Addr(), "", 0, "testprefix:", time.Minute, time.Minute)

	key := "sessid123"
	sess := &sseSession{sessionID: "sessid123"}

	// Test LoadOrStore (should store)
	stored, loaded := store.LoadOrStore(key, sess)
	assert.Equal(t, sess, stored)
	assert.False(t, loaded)

	// Test Load (should hit local cache)
	got, ok := store.Load(key)
	assert.True(t, ok)
	assert.Equal(t, sess.SessionID(), got.SessionID())

	// Test LoadAndDelete
	got2, ok2 := store.LoadAndDelete(key)
	assert.True(t, ok2)
	assert.Equal(t, sess.SessionID(), got2.SessionID())
	store.checkLocalSessions()

	// After delete, should not find
	got3, ok3 := store.Load(key)
	assert.False(t, ok3)
	assert.Nil(t, got3)
}

func TestRedisSessionStore_Range(t *testing.T) {
	s := miniredis.RunT(t)

	store := NewRedisSessionStore(s.Addr(), "", 0, "testprefix:", time.Minute, time.Minute)

	store.LoadOrStore("id1", &sseSession{sessionID: "id1"})
	store.LoadOrStore("id2", &sseSession{sessionID: "id2"})
	store.LoadOrStore("id3", &sseSession{sessionID: "id3"})

	found := make(map[string]bool)
	store.Range(func(key string, sess ClientSession) bool {
		found[key] = true
		return true
	})
	assert.True(t, found["id1"])
	assert.True(t, found["id2"])
	assert.True(t, found["id3"])
}
