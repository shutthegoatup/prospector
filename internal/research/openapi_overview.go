package research

import (
	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/prospecting"
)

func PendingOut(d prospecting.PendingDetail) api.PendingDetail {
	return api.PendingDetail{Id: d.ID, Field: d.Field, Value: d.Value, Current: d.Current, SourceUrl: d.SourceURL, By: d.By, At: d.At, EmailStatus: d.EmailStatus, Vendor: d.Vendor}
}
