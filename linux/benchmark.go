package linux

import (
	"io/ioutil"
	"net/http"
	"time"
)

func benchmark(base string, query string, times int) (time.Duration, error) {
	var sum int64
	var d time.Duration
	url := base + query

	timeout := time.Duration(mirrorTimeout * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	for i := 0; i < times; i++ {
		timer := time.Now()
		response, err := client.Get(url)
		if err != nil {
			return d, err
		}

		defer response.Body.Close()
		_, err = ioutil.ReadAll(response.Body)
		if err != nil {
			return d, err
		}

		sum = sum + int64(time.Since(timer))
	}

	return time.Duration(sum / int64(times)), nil
}
