package money

func DollarsToCents(amount int64) int64 {
	return amount * 100
}

func CentsToDollars(cents int64) int64 {
	return cents / 100
}
