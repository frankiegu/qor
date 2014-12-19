package worker

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/manveru/gostalk/gostalkc"
)

type BeanstalkdQueue struct {
	addr  string
	Debug io.Writer
}

func (bq *BeanstalkdQueue) newClient() (*gostalkc.Client, error) {
	return gostalkc.DialTimeout(bq.addr, time.Minute)
}

// for test
var parseInterval = func(interval uint64) string {
	return strconv.FormatUint(interval*60, 10)
}

func (bq *BeanstalkdQueue) putJob(job *Job) (err error) {
	client, err := bq.newClient()
	if err != nil {
		return
	}

	delay := uint64(job.StartAt.Sub(time.Now()).Seconds())
	jobId := fmt.Sprintf("%d", job.Id)
	interval := parseInterval(job.Interval)
	body := strings.Join([]string{jobId, interval}, ",")
	queueJobId, _, err := client.Put(0, delay, 60, []byte(body))
	if err != nil {
		return
	}

	job.QueueJobId = fmt.Sprint(queueJobId)

	return
}

func NewBeanstalkdQueue(addr string) (bq *BeanstalkdQueue) {
	bq = new(BeanstalkdQueue)
	bq.addr = addr

	return
}

func (bq *BeanstalkdQueue) Enqueue(job *Job) (err error) {
	return bq.putJob(job)
}

func (bq *BeanstalkdQueue) Dequeue() (jobId uint64, err error) {
	for {
		var client *gostalkc.Client
		client, err = bq.newClient()
		if err != nil {
			return
		}
		if err = client.Conn.SetDeadline(time.Now().Add(time.Minute * 5)); err != nil {
			client.Quit()

			if bq.Debug != nil {
				bq.Debug.Write([]byte("beanstalkd: failed to set deadline"))
			}

			continue
		}

		var body []byte
		var queueJobId uint64
		queueJobId, body, err = client.ReserveWithTimeout(300)
		if err != nil {
			if err.Error() == gostalkc.TIMED_OUT {
				if bq.Debug != nil {
					bq.Debug.Write([]byte("beanstalkd: reserve timeout"))
				}
			} else if _, ok := err.(*net.OpError); ok {
				if bq.Debug != nil {
					bq.Debug.Write([]byte("beanstalkd: conn deadline"))
				}
			} else {
				return
			}

			continue
		}

		parts := bytes.Split(body, []byte(","))
		jobId, err = strconv.ParseUint(string(parts[0]), 10, 0)
		if err != nil {
			return
		}

		if bq.Debug != nil {
			fmt.Fprintln(bq.Debug, "beanstalkd: receive job ", jobId)
		}

		if interval := string(parts[1]); interval == "0" {
			if err = client.Delete(queueJobId); err != nil {
				if bq.Debug != nil {
					fmt.Fprintln(bq.Debug, "beanstalkd: delete error", err)
				}
				return
			}
		} else {
			var i uint64
			i, err = strconv.ParseUint(interval, 10, 0)
			if err != nil {
				return
			}
			_, err = client.Release(queueJobId, 0, i)
			if err != nil {
				if bq.Debug != nil {
					fmt.Fprintln(bq.Debug, "beanstalkd: release error", err)
				}
				return
			}
		}

		client.Quit()

		return
	}

	return
}

func (bq *BeanstalkdQueue) Purge(job *Job) (err error) {
	client, err := bq.newClient()
	if err != nil {
		return
	}
	jobId, err := strconv.ParseUint(job.QueueJobId, 10, 0)
	if err != nil {
		return
	}

	err = client.Delete(jobId)

	return
}
