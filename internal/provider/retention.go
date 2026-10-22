package provider

import (
	"time"
	"timone/internal/model"
)

const idleStreamTTL = 2 * time.Minute

func (k *cluster) retainedBytesLocked() int {
	total := 0
	for _, entry := range k.cache {
		total += entry.bytes
	}
	for _, s := range k.streams {
		total += s.retainedBytes
	}
	return total
}
func (k *cluster) expireRetentionLocked(now time.Time) {
	for key, entry := range k.cache {
		if now.Sub(entry.at) >= readLease {
			delete(k.cache, key)
		}
	}
	for key, s := range k.streams {
		if s.cancel != nil && (s.state == "watching" || s.state == "syncing" || s.state == "recovering") && now.Sub(s.lastUsed) > idleStreamTTL {
			s.healthy = false
			s.state = "idle"
			s.message = "Inactive informer retired; next read uses bounded polling"
			delete(k.cache, key)
			s.cancel()
		}
	}
}
func (k *cluster) reserveCacheLocked(bytes int) bool {
	for (k.retainedBytesLocked()+bytes > k.retentionBudget || len(k.cache) >= 256) && len(k.cache) > 0 {
		oldest := ""
		var at time.Time
		for key, entry := range k.cache {
			if oldest == "" || entry.at.Before(at) {
				oldest = key
				at = entry.at
			}
		}
		delete(k.cache, oldest)
	}
	return k.retainedBytesLocked()+bytes <= k.retentionBudget
}
func (k *cluster) retentionWarningLocked(key, message string) {
	if len(k.retentionWarnings) >= 256 {
		k.retentionWarnings = map[string]string{}
	}
	k.retentionWarnings[key] = message
}
func (k *cluster) limitStreamLocked(key string, s *resourceStream) {
	s.healthy = false
	s.state = "limited"
	s.message = "Informer retention budget reached; using bounded polling"
	s.retryAfter = time.Now().Add(time.Minute)
	k.retentionWarningLocked(key, s.message)
	delete(k.cache, key)
	s.cancel()
}
func (k *cluster) reserveStreamObject(key string, s *resourceStream, id string, bytes int) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.streams[key] != s || k.watchCtx == nil || k.watchCtx.Err() != nil {
		return false
	}
	if _, exists := s.objectBytes[id]; !exists && len(s.objectBytes) >= collectionLimit {
		k.limitStreamLocked(key, s)
		return false
	}
	delta := bytes - s.objectBytes[id]
	if s.retainedBytes+delta > 16<<20 || (delta > 0 && !k.reserveCacheLocked(delta)) {
		k.limitStreamLocked(key, s)
		return false
	}
	s.objectBytes[id] = bytes
	s.retainedBytes += delta
	return true
}
func objectIdentity(o model.Object) string {
	return model.ID(model.Str(o, "kind"), model.Str(o, "metadata", "namespace"), model.Str(o, "metadata", "name"))
}
