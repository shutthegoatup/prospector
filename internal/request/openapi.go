package request

import "context"

func ReqFrom(ctx context.Context) *Req { return ctx.Value(ReqKey{}).(*Req) }
