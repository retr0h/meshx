// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

// Package storage owns meshx's bbolt persistence layer. The concrete
// type is *Bolt, returned by New(path); consumers in the meshx package
// cast it to their own Store interface (defined where it's consumed, per
// the osapi-io pattern) so each call site only sees the methods it needs.
//
// Bucket layout:
//
//	meta/
//	  schema_version → "1"
//	radios/
//	  <radio_id>/
//	    messages/ → k=<8-byte-big-endian-seq>, v=json(Message)
//	    nodes/    → k=<node_num-as-string>,    v=json(CachedNode)
//	    settings/ → k=<key>,                  v=<value>
//	ble_devices/
//	  <uuid>      → json(BLEDevice)
//	settings/
//	  <key>       → <value>  (global settings, not radio-scoped)
package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// schema version written into meta/schema_version on first open.
const schemaVersion = "1"

// top-level bucket keys.
var (
	bucketMeta       = []byte("meta")
	bucketRadios     = []byte("radios")
	bucketBLEDevices = []byte("ble_devices")
	bucketSettings   = []byte("settings")

	keySchemaVersion = []byte("schema_version")

	// sub-bucket names under radios/<radio_id>/.
	subMessages = []byte("messages")
	subNodes    = []byte("nodes")
	subSettings = []byte("settings")
)

// DefaultPath returns "$HOME/.meshx/meshx.bolt" with the parent
// directory created on demand. Used by live-radio mode to persist
// chat history across restarts.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	dir := filepath.Join(home, ".meshx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, "meshx.bolt"), nil
}

// RadioIDFromNodeNum returns the canonical Meshtastic identity string
// for a radio: "0x" + lower-cased 8-hex-digit zero-padded node num.
func RadioIDFromNodeNum(myNodeNum uint32) string {
	return fmt.Sprintf("0x%08x", myNodeNum)
}

// PendingRadioID returns the placeholder ID for a connection whose
// my_node_num isn't known yet. Replaced by RadioIDFromNodeNum the moment
// MyNodeInfo arrives — see ClaimRadioIdentity.
func PendingRadioID(transport, addr string) string {
	return fmt.Sprintf("pending:%s:%s", transport, addr)
}

// IsPlaceholderRadioID reports whether id is one of the placeholder
// shapes ClaimRadioIdentity should rewrite on first handshake.
func IsPlaceholderRadioID(id string) bool {
	return strings.HasPrefix(id, "pending:")
}

// ParseRadioDest splits a meshx Dial dest string into transport + addr
// components for radio identity lookup.
//
//	"ble:<uuid>"          → ("ble", "<uuid>")
//	"host:port"           → ("tcp", "host:port")
//	"/dev/cu.usbserial-…" → ("usb", "/dev/cu.usbserial-…")
func ParseRadioDest(dest string) (transport, addr string) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "unknown", "unknown"
	}
	if rest, ok := strings.CutPrefix(dest, "ble:"); ok {
		return "ble", rest
	}
	if i := strings.LastIndex(dest, ":"); i > 0 {
		tail := dest[i+1:]
		isPort := true
		for _, c := range tail {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort && tail != "" {
			return "tcp", dest
		}
	}
	return "usb", dest
}

// Bolt is the concrete bbolt-backed storage implementation.
type Bolt struct {
	db *bolt.DB
}

// New opens (creating if needed) the bbolt file at path, initialises the
// bucket schema, and returns a *Bolt ready for use.
func New(path string) (*Bolt, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt %s: %w", path, err)
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Bolt{db: db}, nil
}

// initSchema ensures every top-level bucket exists and the schema
// version key is written. Idempotent.
func initSchema(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(bucketMeta)
		if err != nil {
			return fmt.Errorf("create meta bucket: %w", err)
		}
		if err := meta.Put(keySchemaVersion, []byte(schemaVersion)); err != nil {
			return fmt.Errorf("write schema version: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketRadios); err != nil {
			return fmt.Errorf("create radios bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketBLEDevices); err != nil {
			return fmt.Errorf("create ble_devices bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSettings); err != nil {
			return fmt.Errorf("create settings bucket: %w", err)
		}
		return nil
	})
}

// radioSubBucket returns (creating if needed) the named sub-bucket under
// radios/<radioID>/. Must be called inside an Update transaction.
func radioSubBucket(
	tx *bolt.Tx,
	radioID string,
	sub []byte,
) (*bolt.Bucket, error) {
	radios := tx.Bucket(bucketRadios)
	if radios == nil {
		return nil, fmt.Errorf("radios bucket missing")
	}
	rb, err := radios.CreateBucketIfNotExists([]byte(radioID))
	if err != nil {
		return nil, fmt.Errorf("create radio bucket %s: %w", radioID, err)
	}
	sb, err := rb.CreateBucketIfNotExists(sub)
	if err != nil {
		return nil, fmt.Errorf("create sub bucket %s/%s: %w", radioID, sub, err)
	}
	return sb, nil
}

// radioSubBucketView returns the named sub-bucket under radios/<radioID>/
// in a read-only fashion. Returns nil without error when the path doesn't
// exist yet — callers treat nil as "no data".
func radioSubBucketView(
	tx *bolt.Tx,
	radioID string,
	sub []byte,
) *bolt.Bucket {
	radios := tx.Bucket(bucketRadios)
	if radios == nil {
		return nil
	}
	rb := radios.Bucket([]byte(radioID))
	if rb == nil {
		return nil
	}
	return rb.Bucket(sub)
}

// Close releases the underlying bbolt handle. Idempotent on nil receiver.
func (b *Bolt) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

// ConsumeBootNotes returns nil — bbolt needs no migration trace.
// Satisfies the radio.Store interface; callers that surface migration
// lines will simply receive nothing.
func (b *Bolt) ConsumeBootNotes() []string {
	return nil
}

// ---- identity ---------------------------------------------------------------

// ResolveRadioByConnection returns the radio_id for the given
// (transport, addr) connection, creating a pending placeholder when the
// radio hasn't been seen before.
//
// Demo mode (b == nil): returns ("", nil).
func (b *Bolt) ResolveRadioByConnection(transport, addr string) (string, error) {
	if b == nil || b.db == nil {
		return "", nil
	}

	// index key under radios/<id>/settings/conn → "<transport>:<addr>"
	// We scan all radios looking for a matching connection record.
	var found string
	err := b.db.View(func(tx *bolt.Tx) error {
		radios := tx.Bucket(bucketRadios)
		if radios == nil {
			return nil
		}
		want := transport + ":" + addr
		return radios.ForEach(func(k, _ []byte) error {
			if found != "" {
				return nil
			}
			rb := radios.Bucket(k)
			if rb == nil {
				return nil
			}
			sb := rb.Bucket(subSettings)
			if sb == nil {
				return nil
			}
			if v := sb.Get([]byte("_conn")); string(v) == want {
				found = string(k)
			}
			return nil
		})
	})
	if err != nil {
		return "", fmt.Errorf("resolve radio: %w", err)
	}
	if found != "" {
		return found, nil
	}

	// Not found — mint a pending placeholder and record the connection.
	pending := PendingRadioID(transport, addr)
	err = b.db.Update(func(tx *bolt.Tx) error {
		sb, err := radioSubBucket(tx, pending, subSettings)
		if err != nil {
			return err
		}
		return sb.Put([]byte("_conn"), []byte(transport+":"+addr))
	})
	if err != nil {
		return "", fmt.Errorf("insert pending radio: %w", err)
	}
	return pending, nil
}

// ClaimRadioIdentity rewrites a pending placeholder radio_id to the
// canonical RadioIDFromNodeNum(myNodeNum) form, migrating all sub-buckets
// (messages, nodes, settings) and the radio bucket key itself.
//
// Returns the new canonical id. No-op when oldID is already canonical.
func (b *Bolt) ClaimRadioIdentity(oldID string, myNodeNum uint32) (string, error) {
	newID := RadioIDFromNodeNum(myNodeNum)
	if !IsPlaceholderRadioID(oldID) {
		return newID, nil
	}
	if b == nil || b.db == nil {
		return newID, nil
	}

	return newID, b.db.Update(func(tx *bolt.Tx) error {
		radios := tx.Bucket(bucketRadios)
		if radios == nil {
			return nil
		}

		// If canonical already exists, drop the placeholder and leave
		// the canonical bucket in place (same merge semantics as the
		// old SQLite implementation).
		if radios.Bucket([]byte(newID)) != nil {
			return radios.DeleteBucket([]byte(oldID))
		}

		// Copy every sub-bucket from oldID into newID.
		oldRB := radios.Bucket([]byte(oldID))
		if oldRB == nil {
			// Nothing to migrate.
			return nil
		}
		newRB, err := radios.CreateBucketIfNotExists([]byte(newID))
		if err != nil {
			return fmt.Errorf("create new radio bucket: %w", err)
		}
		for _, sub := range [][]byte{subMessages, subNodes, subSettings} {
			src := oldRB.Bucket(sub)
			if src == nil {
				continue
			}
			dst, err := newRB.CreateBucketIfNotExists(sub)
			if err != nil {
				return fmt.Errorf("create sub bucket %s: %w", sub, err)
			}
			if err := src.ForEach(func(k, v []byte) error {
				if v == nil {
					// nested bucket — skip (messages/ uses sequences, no nested)
					return nil
				}
				return dst.Put(k, v)
			}); err != nil {
				return fmt.Errorf("copy sub bucket %s: %w", sub, err)
			}
		}
		return radios.DeleteBucket([]byte(oldID))
	})
}

// ---- messages ---------------------------------------------------------------

// seqKey encodes a uint64 as 8 big-endian bytes — used as the bbolt key
// for message rows so they sort chronologically.
func seqKey(seq uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, seq)
	return b
}

// SaveMessage persists one model.Message. System and notice rows are
// skipped. Duplicate packet_id detection is best-effort via a linear
// scan of the existing values (acceptable for the message volumes meshx
// sees — typically < 500 rows).
func (b *Bolt) SaveMessage(radioID, channel string, msg model.Message) error {
	if b == nil || b.db == nil {
		return nil
	}
	if msg.Status == model.StatusSystem || msg.Status == model.StatusNotice {
		return nil
	}

	return b.db.Update(func(tx *bolt.Tx) error {
		sb, err := radioSubBucket(tx, radioID, subMessages)
		if err != nil {
			return err
		}

		// For non-zero packet_id rows: check for an existing record with
		// the same packet_id and update it (status / hops / snr) in place.
		if msg.PacketID != 0 {
			updated, err := updateExistingMessage(sb, channel, msg)
			if err != nil {
				return err
			}
			if updated {
				return nil
			}
		}

		// New row — wrap in the envelope and use the bucket's auto-sequence.
		seq, err := sb.NextSequence()
		if err != nil {
			return fmt.Errorf("next sequence: %w", err)
		}
		env := messageEnvelope{Channel: channel, Message: msg}
		data, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("marshal message: %w", err)
		}
		return sb.Put(seqKey(seq), data)
	})
}

// messageEnvelope wraps model.Message with the channel tag so we can
// filter by channel on load without a separate index bucket.
type messageEnvelope struct {
	Channel string        `json:"ch"`
	Message model.Message `json:"msg"`
}

// updateExistingMessage scans the messages bucket for a record matching
// msg.PacketID+channel and updates its mutable fields. Returns true when
// a match was found and updated.
func updateExistingMessage(
	sb *bolt.Bucket,
	channel string,
	msg model.Message,
) (bool, error) {
	var matchKey []byte
	var existing messageEnvelope

	c := sb.Cursor()
	for k, v := c.Last(); k != nil; k, v = c.Prev() {
		var env messageEnvelope
		if err := json.Unmarshal(v, &env); err != nil {
			continue
		}
		if env.Channel == channel && env.Message.PacketID == msg.PacketID {
			matchKey = make([]byte, len(k))
			copy(matchKey, k)
			existing = env
			break
		}
	}
	if matchKey == nil {
		return false, nil
	}

	// Update only the mutable delivery fields.
	existing.Message.Status = msg.Status
	existing.Message.Hops = msg.Hops
	existing.Message.SNR = msg.SNR
	data, err := json.Marshal(existing)
	if err != nil {
		return false, fmt.Errorf("marshal updated message: %w", err)
	}
	if err := sb.Put(matchKey, data); err != nil {
		return false, fmt.Errorf("update message: %w", err)
	}
	return true, nil
}

// LoadMessages reads the most recent limit rows for the given
// (radioID, channel), oldest-first. channel="" returns messages from
// every channel (used at boot). limit<=0 returns everything.
func (b *Bolt) LoadMessages(
	radioID, channel string,
	limit int,
) ([]model.Message, error) {
	if b == nil || b.db == nil {
		return nil, nil
	}
	if limit == 0 {
		return nil, nil
	}

	var out []model.Message
	err := b.db.View(func(tx *bolt.Tx) error {
		sb := radioSubBucketView(tx, radioID, subMessages)
		if sb == nil {
			return nil
		}

		// Collect the last `limit` matching rows by scanning backward.
		// We'll reverse at the end to return oldest-first.
		var buf []model.Message
		c := sb.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var env messageEnvelope
			if err := json.Unmarshal(v, &env); err != nil {
				continue
			}
			if channel != "" && env.Channel != channel {
				continue
			}
			buf = append(buf, env.Message)
			if limit > 0 && len(buf) >= limit {
				break
			}
		}
		// Reverse so oldest comes first.
		for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
			buf[i], buf[j] = buf[j], buf[i]
		}
		out = buf
		return nil
	})
	return out, err
}

// ExpireStalePendingMessages flips every "pending" row older than ttl to
// "fail". Returns the count updated. Safe on nil receiver.
func (b *Bolt) ExpireStalePendingMessages(radioID string, ttl time.Duration) (int, error) {
	if b == nil || b.db == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	count := 0

	err := b.db.Update(func(tx *bolt.Tx) error {
		sb := radioSubBucketView(tx, radioID, subMessages)
		if sb == nil {
			return nil
		}

		type update struct {
			key []byte
			env messageEnvelope
		}
		var updates []update

		c := sb.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var env messageEnvelope
			if err := json.Unmarshal(v, &env); err != nil {
				continue
			}
			if env.Message.Status != model.StatusPending {
				continue
			}
			if env.Message.SentAt.IsZero() || env.Message.SentAt.After(cutoff) {
				continue
			}
			key := make([]byte, len(k))
			copy(key, k)
			env.Message.Status = model.StatusFail
			updates = append(updates, update{key: key, env: env})
		}

		// Write updates — we need a writable bucket, so re-obtain it via
		// radioSubBucket (which needs the tx, not the read-only view).
		if len(updates) == 0 {
			return nil
		}
		wsb, err := radioSubBucket(tx, radioID, subMessages)
		if err != nil {
			return err
		}
		for _, u := range updates {
			data, err := json.Marshal(u.env)
			if err != nil {
				return fmt.Errorf("marshal expired message: %w", err)
			}
			if err := wsb.Put(u.key, data); err != nil {
				return fmt.Errorf("write expired message: %w", err)
			}
			count++
		}
		return nil
	})
	return count, err
}

// ---- nodes ------------------------------------------------------------------

// SaveNode persists a peer's identity. Placeholder callsigns (both
// names empty) are skipped.
func (b *Bolt) SaveNode(radioID string, n model.CachedNode) error {
	if b == nil || b.db == nil {
		return nil
	}
	if n.LongName == "" && n.ShortName == "" {
		return nil
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		sb, err := radioSubBucket(tx, radioID, subNodes)
		if err != nil {
			return err
		}
		data, err := json.Marshal(n)
		if err != nil {
			return fmt.Errorf("marshal node: %w", err)
		}
		key := []byte(fmt.Sprintf("%d", n.NodeNum))
		return sb.Put(key, data)
	})
}

// LoadNodes reads every persisted node for radioID.
func (b *Bolt) LoadNodes(radioID string) ([]model.CachedNode, error) {
	if b == nil || b.db == nil {
		return nil, nil
	}
	var out []model.CachedNode
	err := b.db.View(func(tx *bolt.Tx) error {
		sb := radioSubBucketView(tx, radioID, subNodes)
		if sb == nil {
			return nil
		}
		return sb.ForEach(func(_, v []byte) error {
			var n model.CachedNode
			if err := json.Unmarshal(v, &n); err != nil {
				return fmt.Errorf("unmarshal node: %w", err)
			}
			out = append(out, n)
			return nil
		})
	})
	return out, err
}

// SaveNodePrefs writes just the sticky UX preferences (favorite / muted)
// for a single node num. INSERT-or-update so this works even when the
// identity row isn't saved yet (user stars a still-ghost peer).
func (b *Bolt) SaveNodePrefs(
	radioID string,
	nodeNum uint32,
	favorite, muted bool,
) error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		sb, err := radioSubBucket(tx, radioID, subNodes)
		if err != nil {
			return err
		}
		key := []byte(fmt.Sprintf("%d", nodeNum))
		var n model.CachedNode
		if v := sb.Get(key); v != nil {
			if err := json.Unmarshal(v, &n); err != nil {
				return fmt.Errorf("unmarshal node prefs: %w", err)
			}
		} else {
			n.NodeNum = nodeNum
		}
		n.Favorite = favorite
		n.Muted = muted
		data, err := json.Marshal(n)
		if err != nil {
			return fmt.Errorf("marshal node prefs: %w", err)
		}
		return sb.Put(key, data)
	})
}

// ---- settings ---------------------------------------------------------------

// GetSetting returns the persisted value for key or ("", false) if absent.
// radioID="" queries the global settings bucket.
func (b *Bolt) GetSetting(radioID, key string) (string, bool, error) {
	if b == nil || b.db == nil {
		return "", false, nil
	}
	var value string
	var found bool
	err := b.db.View(func(tx *bolt.Tx) error {
		var bkt *bolt.Bucket
		if radioID == "" {
			bkt = tx.Bucket(bucketSettings)
		} else {
			bkt = radioSubBucketView(tx, radioID, subSettings)
		}
		if bkt == nil {
			return nil
		}
		v := bkt.Get([]byte(key))
		if v != nil {
			value = string(v)
			found = true
		}
		return nil
	})
	return value, found, err
}

// PutSetting writes value under (key, radioID). radioID="" writes to the
// global settings bucket.
func (b *Bolt) PutSetting(radioID, key, value string) error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		var bkt *bolt.Bucket
		var err error
		if radioID == "" {
			bkt = tx.Bucket(bucketSettings)
			if bkt == nil {
				return fmt.Errorf("settings bucket missing")
			}
		} else {
			bkt, err = radioSubBucket(tx, radioID, subSettings)
			if err != nil {
				return err
			}
		}
		return bkt.Put([]byte(key), []byte(value))
	})
}

// ---- BLE devices ------------------------------------------------------------

// SaveBLEDevice inserts or updates a paired BLE device. Does NOT touch
// the favorite flag — SetBLEFavorite is the single entrypoint for that.
func (b *Bolt) SaveBLEDevice(d model.BLEDevice) error {
	if b == nil || b.db == nil {
		return nil
	}
	if d.UUID == "" {
		return fmt.Errorf("save ble device: uuid required")
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketBLEDevices)
		if bkt == nil {
			return fmt.Errorf("ble_devices bucket missing")
		}
		// Preserve existing favorite flag when updating.
		var existing model.BLEDevice
		if v := bkt.Get([]byte(d.UUID)); v != nil {
			_ = json.Unmarshal(v, &existing)
			d.Favorite = existing.Favorite
		}
		data, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal ble device: %w", err)
		}
		return bkt.Put([]byte(d.UUID), data)
	})
}

// LoadBLEDevices returns every saved Bluetooth device. Favorites appear
// first (matching the SQLite ORDER BY favorite DESC, paired_at DESC intent
// — bbolt doesn't have SQL ordering, so we do a two-pass partition).
func (b *Bolt) LoadBLEDevices() ([]model.BLEDevice, error) {
	if b == nil || b.db == nil {
		return nil, nil
	}
	var favs, rest []model.BLEDevice
	err := b.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketBLEDevices)
		if bkt == nil {
			return nil
		}
		return bkt.ForEach(func(_, v []byte) error {
			var d model.BLEDevice
			if err := json.Unmarshal(v, &d); err != nil {
				return fmt.Errorf("unmarshal ble device: %w", err)
			}
			if d.Favorite {
				favs = append(favs, d)
			} else {
				rest = append(rest, d)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return append(favs, rest...), nil
}

// LookupBLEDevice finds a saved device by exact UUID, longname, or
// shortname (case-insensitive). Returns nil when not found.
func (b *Bolt) LookupBLEDevice(needle string) (*model.BLEDevice, error) {
	devs, err := b.LoadBLEDevices()
	if err != nil {
		return nil, err
	}
	lowered := strings.ToLower(needle)
	for _, d := range devs {
		d := d
		if d.UUID == needle || strings.ToLower(d.UUID) == lowered {
			return &d, nil
		}
		if strings.EqualFold(d.LongName, needle) || strings.EqualFold(d.ShortName, needle) {
			return &d, nil
		}
	}
	return nil, nil
}

// SetBLEFavorite marks exactly one device as the auto-connect favorite,
// clearing the flag on every other row atomically. Empty uuid clears all.
func (b *Bolt) SetBLEFavorite(uuid string) error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketBLEDevices)
		if bkt == nil {
			return nil
		}
		return bkt.ForEach(func(k, v []byte) error {
			var d model.BLEDevice
			if err := json.Unmarshal(v, &d); err != nil {
				return fmt.Errorf("unmarshal ble device: %w", err)
			}
			want := uuid != "" && string(k) == uuid
			if d.Favorite == want {
				return nil
			}
			d.Favorite = want
			data, err := json.Marshal(d)
			if err != nil {
				return fmt.Errorf("marshal ble device: %w", err)
			}
			return bkt.Put(k, data)
		})
	})
}

// ForgetBLEDevice removes a paired device by UUID. Idempotent.
func (b *Bolt) ForgetBLEDevice(uuid string) error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketBLEDevices)
		if bkt == nil {
			return nil
		}
		return bkt.Delete([]byte(uuid))
	})
}
