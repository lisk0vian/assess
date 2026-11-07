// Throwaway: dumps real ntcharts/qrterminal cells as JSON for the design canvas.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"
	"github.com/mdp/qrterminal/v3"
)

// Sentinel colours stand for tokens so one dump serves both themes.
var (
	ink  = lipgloss.NewStyle().Foreground(lipgloss.Color("#010101"))
	ink2 = lipgloss.NewStyle().Foreground(lipgloss.Color("#020202"))
)

type run struct {
	T string `json:"t"`
	R string `json:"r"` // role
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// parse splits ANSI lines into runs of text with their token role.
func parse(s string) [][]run {
	var out [][]run
	for _, l := range strings.Split(s, "\n") {
		var rs []run
		role := "ink"
		pos := 0
		for _, m := range sgr.FindAllStringIndex(l, -1) {
			if m[0] > pos {
				rs = append(rs, run{l[pos:m[0]], role})
			}
			code := l[m[0]:m[1]]
			switch {
			case strings.Contains(code, "2;1;1;1") || strings.Contains(code, "2:1:1:1"):
				role = "ink"
			case strings.Contains(code, "2;2;2;2") || strings.Contains(code, "2:2:2:2"):
				role = "ink2"
			}
			pos = m[1]
		}
		rs = append(rs, run{l[pos:], role})
		out = append(out, rs)
	}
	return out
}

func hist(bw, h int) string {
	vals := []float64{0, 1, 1, 3, 4, 6, 5, 3, 1, 0}
	labels := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	data := make([]barchart.BarData, len(vals))
	for i, v := range vals {
		data[i] = barchart.BarData{Label: labels[i], Values: []barchart.BarValue{{Name: "n", Value: v, Style: ink}}}
	}
	ch := barchart.New(len(vals)*(bw+1), h, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(bw), barchart.WithBarGap(1),
		barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(6), barchart.WithStyles(ink2, ink2), barchart.WithDataSet(data))
	ch.Draw()
	return ch.View()
}

func timeline(n, h int) string {
	warn := []float64{0, 1, 0, 2, 1, 3, 1, 0, 2, 4, 1, 2, 0, 1, 3, 1, 0, 1, 2, 0}
	high := []float64{0, 0, 0, 0, 1, 0, 0, 0, 1, 2, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0}
	data := make([]barchart.BarData, n)
	for i := range n {
		data[i] = barchart.BarData{Values: []barchart.BarValue{
			{Name: "warn", Value: warn[i%20], Style: ink2}, {Name: "high", Value: high[i%20], Style: ink}}}
	}
	ch := barchart.New(n*3, h, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(2), barchart.WithBarGap(1),
		barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(6), barchart.WithStyles(ink2, ink2), barchart.WithDataSet(data))
	ch.Draw()
	return ch.View()
}

func questions() string {
	rates := []float64{92, 88, 76, 71, 54, 83, 39, 67, 28, 61}
	data := make([]barchart.BarData, len(rates))
	for i, r := range rates {
		data[i] = barchart.BarData{Label: fmt.Sprintf("P%02d ", i+1), Values: []barchart.BarValue{{Name: "p", Value: r, Style: ink}}}
	}
	ch := barchart.New(44, len(rates), barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(100),
		barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(1), barchart.WithBarGap(0), barchart.WithStyles(ink2, ink2),
		barchart.WithDataSet(data), barchart.WithHorizontalBars())
	ch.Draw()
	return ch.View()
}

func spark(w, h int, vals []float64) string {
	s := sparkline.New(w, h, sparkline.WithStyle(ink), sparkline.WithMaxValue(24))
	s.PushAll(vals)
	s.Draw()
	return s.View()
}

func qr() string {
	var b strings.Builder
	qrterminal.GenerateWithConfig("http://192.168.1.20:27337/?c=K7M2QP", qrterminal.Config{
		Level: qrterminal.L, Writer: &b, HalfBlocks: true, QuietZone: 1,
		BlackChar: qrterminal.BLACK_BLACK, BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteChar: qrterminal.WHITE_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK,
	})
	return strings.TrimRight(b.String(), "\n")
}

func main() {
	sub := make([]float64, 40)
	for i := range sub {
		if i > 24 {
			sub[i] = float64(i-24) * 0.45
		}
	}
	on := make([]float64, 40)
	for i := range on {
		on[i] = min(24, float64(i)*3)
		if i == 27 || i == 28 {
			on[i] = 21
		}
	}
	out := map[string]any{
		"hist": parse(hist(6, 8)), "histS": parse(hist(4, 6)), "timeline": parse(timeline(20, 6)), "timelineS": parse(timeline(12, 4)),
		"questions": parse(questions()), "sparkSub": parse(spark(40, 3, sub)), "sparkOn": parse(spark(40, 3, on)),
		"qr": qr(),
	}
	f, _ := os.Create("cells.json")
	defer f.Close()
	if err := json.NewEncoder(f).Encode(out); err != nil {
		panic(err)
	}
}
