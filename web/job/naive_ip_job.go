package job

import (
	"github.com/mhsanaei/3x-ui/v3/web/service"
)

// NaiveIpJob enforces per-user IP limits for naive servers by reading their
// access logs and banning excess IPs via nftables. Holds the web layer's
// NaiveService instance so it sees live server state.
type NaiveIpJob struct {
	naiveService *service.NaiveService
}

func NewNaiveIpJob(naiveService *service.NaiveService) *NaiveIpJob {
	return &NaiveIpJob{naiveService: naiveService}
}

// Run is the cron.Job interface method invoked on schedule.
func (j *NaiveIpJob) Run() {
	if j.naiveService == nil {
		return
	}
	j.naiveService.EnforceIPLimits()
}
