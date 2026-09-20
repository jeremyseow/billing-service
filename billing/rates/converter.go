package rates

import (
	"errors"

	"github.com/shopspring/decimal"
)

func ConvertRates(sourceCurrency, targetCurrency string, sourceAmount decimal.Decimal) (decimal.Decimal, decimal.Decimal, error) {
	// hardcoded but should be populated by some external rates service
	rates := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(1),
		"GEL": decimal.NewFromFloat(2.89),
	}

	sourceRate, sourceExists := rates[sourceCurrency]
	if !sourceExists {
		return decimal.Zero, decimal.Zero, errors.New("source currency not found")
	}
	targetRate, targetExists := rates[targetCurrency]
	if !targetExists {
		return decimal.Zero, decimal.Zero, errors.New("target currency not found")
	}

	fxRate := targetRate.Div(sourceRate)
	convertedAmount := sourceAmount.Mul(fxRate)
	return convertedAmount, fxRate, nil
}
