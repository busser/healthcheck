package healthcheck

import (
	"net"
	"net/http"
	"time"
)

// A Service can be contacted at a specific network endpoint.
type Service interface {
	// Addr provides the Service's network endpoint.
	Addr() net.Addr
}

type response struct {
	svc    Service
	status bool
}

// Run checks whether services are healthy.
func Run(services []Service, timeout time.Duration) map[net.Addr]bool {
	statusByAddr := make(map[net.Addr]bool)

	responses := make(chan response, len(services))
	defer close(responses)

	for _, svc := range services {
		go func(svc Service) {
			responses <- response{svc, checkService(svc, timeout)}
		}(svc)
	}

	for i := 0; i < len(services); i++ {
		resp := <-responses
		statusByAddr[resp.svc.Addr()] = resp.status
	}

	return statusByAddr
}

func checkService(svc Service, timeout time.Duration) bool {
	client := &http.Client{
		Timeout: timeout,
	}

	url := "http://" + svc.Addr().String() + "/status"

	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	return true
}
