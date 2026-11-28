// Package webhooks sends an organisation's events to the endpoints its owners set up,
// each as the key it is bound to may read them, signed as Standard Webhooks sign them.
package webhooks

import (
	"crypto/rand"
	"encoding/base64"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/egress"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// maxEndpoints bounds what one organisation sets up.
const maxEndpoints = 20

// Overlap is how long a replaced secret still signs, beside the new one.
const Overlap = 24 * time.Hour

type WebhookStore struct{ *store.Core }

// newSecret is a signing secret as Standard Webhooks' libraries take it.
func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "whsec_" + base64.StdEncoding.EncodeToString(b)
}

// checkURL refuses an address that isn't https on the public internet. A name is
// judged again as each delivery connects; one written as an address is judged now.
func checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", say.Errorf("an endpoint is an https address, such as https://example.com/hooks, not %q", raw)
	}
	host := u.Hostname()
	ip, err := netip.ParseAddr(host)
	public := err == nil && egress.Public(ip) || err != nil && strings.Contains(host, ".") && !strings.EqualFold(host, "localhost")
	if !public {
		return "", say.Errorf("%s isn't on the public internet", host)
	}
	return u.String(), nil
}

func checkEvents(events []string) (string, error) {
	if len(events) == 0 {
		return "", say.New("name at least one event to send")
	}
	var out []string
	for _, e := range events {
		e = strings.TrimSpace(e)
		if !store.KnownEventType(e) {
			return "", say.Errorf("no event %q: GET /webhook-events lists them", e)
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return strings.Join(out, " "), nil
}

// readingKey refuses a key an endpoint can't be bound to: not the organisation's, revoked,
// or unable to read.
func readingKey(tx *gorm.DB, org, id int64) error {
	var k store.APIKey
	if err := tx.Where("org_id = ? and id = ? and revoked_at is null", org, id).First(&k).Error; err != nil {
		return store.Missing(err, "no key %d in this organisation", id)
	}
	if !k.Has("read") {
		return say.Errorf("the key %s can't read, so it would be sent nothing: give it the read scope", k.Name)
	}
	return nil
}

func (s *WebhookStore) Webhooks(org int64) ([]store.WebhookEndpoint, error) {
	var out []store.WebhookEndpoint
	return out, s.InOrg(org).Order("id desc").Find(&out).Error
}

func (s *WebhookStore) Webhook(org, id int64) (*store.WebhookEndpoint, error) {
	var e store.WebhookEndpoint
	if err := s.InOrg(org).Where("id = ?", id).First(&e).Error; err != nil {
		return nil, store.Missing(err, "no webhook %d in this organisation", id)
	}
	return &e, nil
}

// AddWebhook sets up an endpoint and gives back its secret, which isn't shown again.
func (s *WebhookStore) AddWebhook(org, key int64, rawURL string, events []string, actor string) (*store.WebhookEndpoint, string, error) {
	if s.Keyring == nil {
		return nil, "", say.New("a webhook's secret has to be kept sealed, and this server has no PROSPECTOR_SECRET_KEYS")
	}
	u, err := checkURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	kinds, err := checkEvents(events)
	if err != nil {
		return nil, "", err
	}
	secret := newSecret()
	sealed, err := s.Keyring.Seal(secret)
	if err != nil {
		return nil, "", err
	}
	e := store.WebhookEndpoint{OrgID: org, KeyID: key, URL: u, Events: kinds, Secret: sealed, CreatedBy: actor, CreatedAt: time.Now()}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.HoldOrg(tx, org); err != nil {
			return err
		}
		if err := readingKey(tx, org, key); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&store.WebhookEndpoint{}).Where("org_id = ?", org).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxEndpoints {
			return say.Errorf("an organisation can send to %d endpoints", maxEndpoints)
		}
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		if err := tx.Create(&store.EndpointKey{OrgID: org, EndpointID: e.ID, Kid: store.RandomToken(8), Secret: sealed, CreatedAt: e.CreatedAt}).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActEndpointNew, &u)
	})
	if err != nil {
		return nil, "", err
	}
	return &e, secret, nil
}

// WebhookChange is what an owner changes of an endpoint; nil is left as it is.
type WebhookChange struct {
	URL     *string
	Events  *[]string
	Enabled *bool
}

func (s *WebhookStore) ChangeWebhook(org, id int64, c WebhookChange, actor string) (*store.WebhookEndpoint, error) {
	var e store.WebhookEndpoint
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&e).Error; err != nil {
			return store.Missing(err, "no webhook %d in this organisation", id)
		}
		updates := map[string]any{}
		if c.URL != nil {
			u, err := checkURL(*c.URL)
			if err != nil {
				return err
			}
			updates["url"] = u
		}
		if c.Events != nil {
			kinds, err := checkEvents(*c.Events)
			if err != nil {
				return err
			}
			updates["events"] = kinds
		}
		if c.Enabled != nil {
			if *c.Enabled {
				if err := readingKey(tx, org, e.KeyID); err != nil {
					return err
				}
				updates["disabled_at"], updates["disabled_why"] = nil, ""
			} else if e.DisabledAt == nil {
				updates["disabled_at"], updates["disabled_why"] = time.Now(), "stopped by "+actor
			}
		}
		if len(updates) == 0 {
			return nil
		}
		if err := tx.Model(&e).Updates(updates).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActEndpointSet, &e.URL)
	})
	if err != nil {
		return nil, err
	}
	return s.Webhook(org, id)
}

func (s *WebhookStore) DeleteWebhook(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var e store.WebhookEndpoint
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&e).Error; err != nil {
			return store.Missing(err, "no webhook %d in this organisation", id)
		}
		if err := tx.Delete(&e).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and endpoint_id = ?", org, id).Delete(&store.EndpointKey{}).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActEndpointDrop, &e.URL)
	})
}

// RotateWebhookSecret gives an endpoint a new secret; the one it replaces signs too for
// Overlap. It gives back the new one, which isn't shown again.
func (s *WebhookStore) RotateWebhookSecret(org, id int64, actor string) (string, error) {
	if s.Keyring == nil {
		return "", say.New("a webhook's secret has to be kept sealed, and this server has no PROSPECTOR_SECRET_KEYS")
	}
	secret := newSecret()
	sealed, err := s.Keyring.Seal(secret)
	if err != nil {
		return "", err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var e store.WebhookEndpoint
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&e).Error; err != nil {
			return store.Missing(err, "no webhook %d in this organisation", id)
		}
		now := time.Now()
		until := now.Add(Overlap)
		if err := tx.Where("org_id = ? and endpoint_id = ? and expires_at <= ?", org, id, now).Delete(&store.EndpointKey{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.EndpointKey{}).Where("org_id = ? and endpoint_id = ? and expires_at is null", org, id).Update("expires_at", until).Error; err != nil {
			return err
		}
		if err := tx.Create(&store.EndpointKey{OrgID: org, EndpointID: id, Kid: store.RandomToken(8), Secret: sealed, CreatedAt: now}).Error; err != nil {
			return err
		}
		// The release before the key set signs with these columns, kept in step for a rollback.
		if err := tx.Model(&e).Updates(map[string]any{"secret": sealed, "previous_secret": e.Secret, "previous_until": until}).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActEndpointKey, &e.URL)
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

// KeyNames are the names of the organisation's keys, revoked ones too, by id.
func (s *WebhookStore) KeyNames(org int64) (map[int64]string, error) {
	var keys []store.APIKey
	if err := s.InOrg(org).Select("id, name").Find(&keys).Error; err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, k := range keys {
		out[k.ID] = k.Name
	}
	return out, nil
}
