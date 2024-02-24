package clevelandart

import (
	"fmt"
	"io"
	"net/http"
)

func GetArtwork(id string) ([]byte, error) {
	// fetch an artwork from CMA Open API
	const endpoint = "https://openaccess-api.clevelandart.org/api/"
	resp, err := http.Get(fmt.Sprintf(endpoint+"artworks/%v", id))
	if err != nil {
		return nil, fmt.Errorf("error: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("could not fetch data: %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %v", err)
	}

	return b, nil
}
