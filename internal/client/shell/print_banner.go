package shell

import figure "github.com/common-nighthawk/go-figure"

func printBanner() {
	figure.NewFigure(
		shellBannerText,
		"",
		true,
	).Print()
}
