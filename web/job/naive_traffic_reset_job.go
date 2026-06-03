package job

import (
	"github.com/mhsanaei/3x-ui/v3/logger"
	"github.com/mhsanaei/3x-ui/v3/web/service"
)

// NaiveTrafficResetJob zeroes naive server counters on a schedule (day / week /
// month), mirroring the inbound periodic reset. Enabled, non-expired servers
// that were stopped on quota are resumed by the service after the reset.
type NaiveTrafficResetJob struct {
	naiveService *service.NaiveService
	period       string
}

func NewNaiveTrafficResetJob(naiveService *service.NaiveService, period string) *NaiveTrafficResetJob {
	return &NaiveTrafficResetJob{naiveService: naiveService, period: period}
}

func (j *NaiveTrafficResetJob) Run() {
	if j.naiveService == nil {
		return
	}
	n, err := j.naiveService.ResetTrafficBySchedule(j.period)
	if err != nil {
		logger.Warningf("naive traffic reset (%s): %v", j.period, err)
		return
	}
	if n > 0 {
		logger.Infof("naive traffic reset (%s): %d servers", j.period, n)
	}
}
