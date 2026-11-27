package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ProofLife is how long going through an organisation's provider counts for. After it
// the provider is asked again, so somebody it has since turned away is out within a day.
const ProofLife = 24 * time.Hour

// Proofs reads SSOOrgs: each organisation with when its proof runs out, as ",3:1795600000,".
func (sess *Session) Proofs() map[int64]int64 {
	out := map[int64]int64{}
	for _, part := range strings.Split(sess.SSOOrgs, ",") {
		org, until, ok := strings.Cut(part, ":")
		o, err1 := strconv.ParseInt(org, 10, 64)
		u, err2 := strconv.ParseInt(until, 10, 64)
		if ok && err1 == nil && err2 == nil {
			out[o] = u
		}
	}
	return out
}

func (sess *Session) Prove(org int64, now time.Time) {
	proofs := sess.Proofs()
	proofs[org] = now.Add(ProofLife).Unix()
	sess.SSOOrgs = ","
	for o, until := range proofs {
		if until > now.Unix() {
			sess.SSOOrgs += fmt.Sprintf("%d:%d,", o, until)
		}
	}
}

func (sess *Session) Proves(org int64) bool { return sess.Proofs()[org] > time.Now().Unix() }

func (sess *Session) Opens(org int64) bool { return !sess.Limited || sess.Proves(org) }

func (k *APIKey) Has(scope string) bool {
	for _, s := range strings.Split(k.Scopes, " ") {
		if s == scope {
			return true
		}
	}
	return false
}
