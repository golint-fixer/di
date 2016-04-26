package scheduler

import (
	"fmt"
	"strings"
	"sync"

	"github.com/NetSys/di/db"
	"github.com/NetSys/di/minion/docker"

	log "github.com/Sirupsen/logrus"
)

type swarm struct {
	dk docker.Client
}

func newSwarm(dk docker.Client) scheduler {
	return swarm{dk}
}

func (s swarm) list() ([]docker.Container, error) {
	return s.dk.List(map[string][]string{"label": {docker.SchedulerLabelPair}})
}

func (s swarm) boot(dbcs []db.Container) {
	var wg sync.WaitGroup
	wg.Add(len(dbcs))

	logChn := make(chan string, 1)
	for _, dbc := range dbcs {
		dbc := dbc
		go func() {
			labels := makeLabels(dbc)
			env := makeEnv(dbc)
			err := s.dk.Run(docker.RunOptions{
				Image:  dbc.Image,
				Args:   dbc.Command,
				Env:    env,
				Labels: labels,
			})
			if err != nil {
				msg := fmt.Sprintf("Failed to start container %s: %s",
					dbc.Image, err)
				select {
				case logChn <- msg:
				default:
				}
			} else {
				log.Infof("Started container: %s %s", dbc.Image,
					strings.Join(dbc.Command, " "))
			}
			wg.Done()
		}()
	}

	wg.Wait()

	select {
	case msg := <-logChn:
		log.Warning(msg)
	default:
	}
}

func makeLabels(dbc db.Container) map[string]string {
	labels := map[string]string{
		docker.SchedulerLabelKey: docker.SchedulerLabelValue,
	}
	for _, lb := range dbc.Labels {
		labels[docker.UserLabel(lb)] = docker.LabelTrueValue
	}
	return labels
}

func makeEnv(dbc db.Container) map[string]struct{} {
	env := make(map[string]struct{})
	for _, label := range dbc.Labels {
		for excludeLabels := range dbc.Placement.Exclusive {
			if excludeLabels[0] == label {
				affinityStr := fmt.Sprintf("affinity:%s!=%s",
					docker.UserLabel(excludeLabels[1]),
					docker.LabelTrueValue)
				env[affinityStr] = struct{}{}
			} else if excludeLabels[1] == label {
				affinityStr := fmt.Sprintf("affinity:%s!=%s",
					docker.UserLabel(excludeLabels[0]),
					docker.LabelTrueValue)
				env[affinityStr] = struct{}{}
			}
		}
	}
	for key, value := range dbc.Env {
		envStr := fmt.Sprintf("%s=%s", key, value)
		env[envStr] = struct{}{}
	}
	return env
}

func (s swarm) terminate(ids []string) {
	var wg sync.WaitGroup
	wg.Add(len(ids))
	defer wg.Wait()
	for _, id := range ids {
		id := id
		go func() {
			err := s.dk.RemoveID(id)
			if err != nil {
				log.WithError(err).Warn("Failed to stop container.")
			}
			wg.Done()
		}()
	}
}
