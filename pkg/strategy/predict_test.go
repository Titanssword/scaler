package strategy

import (
	"testing"
)

func TestArima(t *testing.T) {
	// dataArray := []float64{2, 1, 2, 5, 2, 1, 2, 5, 2, 1, 2, 5, 2, 1, 2, 5}
	// dataArray := []float64{2, 1, 2, 5, 2, 1, 2, 5, 2, 1, 2, 5, 2, 1, 2, 5}
	// // trueForecast := []float64{2, 1, 2, 5}
	// // Set ARIMA model parameters.
	// p := 4
	// d := 1
	// q := 2
	// P := 1
	// D := 1
	// Q := 0
	// m := 0
	// forecastSize := 10
	// forecastResult := arima.ForeCastARIMA(dataArray, forecastSize, arima.NewConfig(p, d, q, P, D, Q, m))
	// forecast := forecastResult.GetForecast()
	// upper := forecastResult.GetForecastUpperConf()
	// lower := forecastResult.GetForecastLowerConf()

	// log.Debug("rmse: ", commonTestCalculateRMSE("test", dataArray, trueForecast, forecastSize, p, d, q, P, D, Q, m))
}
