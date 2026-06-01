package job

import (
	"github.com/mhsanaei/3x-ui/v3/logger"
	"github.com/mhsanaei/3x-ui/v3/web/service"
)

// NaiveTrafficJob periodically samples per-server naive traffic from the kernel
// (nftables counters) into the database. It holds the same NaiveService
// instance the web layer owns so it sees the live process/server state.
type NaiveTrafficJob struct {
	naiveService *service.NaiveService
}

func NewNaiveTrafficJob(naiveService *service.NaiveService) *NaiveTrafficJob {
	return &NaiveTrafficJob{naiveService: naiveService}
}

// Run is the cron.Job interface method invoked on schedule.
func (j *NaiveTrafficJob) Run() {
	if j.naiveService == nil {
		return
	}
	if err := j.naiveService.SampleTraffic(); err != nil {
		logger.Warningf("naive traffic job: %v", err)
	}
}
