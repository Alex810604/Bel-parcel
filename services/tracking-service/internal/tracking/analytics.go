package tracking

import (
	"math"
	"time"
)

func calculateDeviation(lat, lng, originLat, originLng, destLat, destLng float64) float64 {
	// Distance from point to line segment (origin -> destination)
	ax := originLat
	ay := originLng
	bx := destLat
	by := destLng
	px := lat
	py := lng
	abx := bx - ax
	aby := by - ay
	apx := px - ax
	apy := py - ay
	ab2 := abx*abx + aby*aby
	if ab2 == 0 {
		return haversine(lat, lng, originLat, originLng)
	}
	t := (apx*abx + apy*aby) / ab2
	if t < 0 {
		return haversine(lat, lng, originLat, originLng)
	} else if t > 1 {
		return haversine(lat, lng, destLat, destLng)
	}
	projx := ax + t*abx
	projy := ay + t*aby
	return haversine(lat, lng, projx, projy)
}

func calculateEstimatedArrival(assignedAt time.Time, estimatedDuration string, currentLat, currentLng, destLat, destLng float64, totalRouteDist float64) time.Time {
	remainingDist := haversine(currentLat, currentLng, destLat, destLng)
	if totalRouteDist < 1.0 {
		return time.Now().UTC()
	}
	remDuration := time.Duration(float64(parseDuration(estimatedDuration)) * (remainingDist / totalRouteDist))
	return time.Now().UTC().Add(remDuration)
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dphi := (lat2 - lat1) * math.Pi / 180
	dl := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dphi/2)*math.Sin(dphi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(dl/2)*math.Sin(dl/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}
