package data_quality

// DetectTaxAnomaly returns true when buy/sell tax exceeds configured ceilings.
func DetectTaxAnomaly(buyTaxBps, sellTaxBps, maxBuyTaxBps, maxSellTaxBps int32) bool {
	if buyTaxBps > maxBuyTaxBps {
		return true
	}
	if sellTaxBps > maxSellTaxBps {
		return true
	}
	return false
}
