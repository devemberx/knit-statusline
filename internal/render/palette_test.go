package render

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// Floor style.go declare: 18.4 where Orange and Yellow already sit, closest
// pair on row. Rule live in comment, nothing else enforce it, so new entry
// landing beside old one surface first on somebody's terminal.
const paletteFloor = 18.4

// Constants out of reach of reflection, so read style.go itself. Color added
// without touching this file still get measured.
var paletteEntry = regexp.MustCompile(`(\w+)\s+Color = "\\033\[38;2;(\d+);(\d+);(\d+)m"`)

type swatch struct {
	name    string
	r, g, b float64
}

func palette(t *testing.T) []swatch {
	t.Helper()
	src, err := os.ReadFile("style.go")
	if err != nil {
		t.Fatalf("read palette source: %v", err)
	}
	// Dim is SGR 2, no foreground of its own, so it never match. Every other
	// entry must -- miss here read as "palette clean" while measuring nothing.
	found := paletteEntry.FindAllStringSubmatch(string(src), -1)
	if len(found) < 10 {
		t.Fatalf("matched %d palette entries in style.go, pattern out of step", len(found))
	}
	out := make([]swatch, 0, len(found))
	for _, e := range found {
		out = append(out, swatch{e[1], channel(t, e[2]), channel(t, e[3]), channel(t, e[4])})
	}
	return out
}

func channel(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil || v > 255 {
		t.Fatalf("channel %q out of 0-255: %v", s, err)
	}
	return float64(v)
}

func TestPaletteEntriesStayApart(t *testing.T) {
	entries := palette(t)
	for i, a := range entries {
		for _, b := range entries[i+1:] {
			if d := deltaE(lab(a), lab(b)); d < paletteFloor {
				t.Errorf("%s and %s sit at CIEDE2000 %.2f, floor is %.1f",
					a.name, b.name, d, paletteFloor)
			}
		}
	}
}

// Colorimetry wrong in same direction for every pair still clear floor check,
// so pin deltaE against Sharma published CIEDE2000 test data.
func TestDeltaEMatchesReferenceData(t *testing.T) {
	for _, tc := range []struct {
		a, b [3]float64
		want float64
	}{
		{[3]float64{50, 2.6772, -79.7751}, [3]float64{50, 0, -82.7485}, 2.0425},
		{[3]float64{50, 2.5, 0}, [3]float64{73, 25, -18}, 27.1492},
		{[3]float64{50, 2.5, 0}, [3]float64{50, 3.1736, 0.5854}, 1.0000},
	} {
		if got := deltaE(tc.a, tc.b); math.Abs(got-tc.want) > 0.0001 {
			t.Errorf("deltaE(%v, %v) = %.4f, want %.4f", tc.a, tc.b, got, tc.want)
		}
	}
}

// lab convert sRGB swatch to CIELAB under D65, white point terminal colors
// authored against.
func lab(s swatch) [3]float64 {
	r, g, b := linear(s.r), linear(s.g), linear(s.b)
	x := (r*0.4124564 + g*0.3575761 + b*0.1804375) / 0.95047
	y := r*0.2126729 + g*0.7151522 + b*0.0721750
	z := (r*0.0193339 + g*0.1191920 + b*0.9503041) / 1.08883
	fx, fy, fz := pivot(x), pivot(y), pivot(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

func linear(c float64) float64 {
	c /= 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// Linear segment near black keep cube root off its vertical tangent.
func pivot(t float64) float64 {
	const d = 6.0 / 29.0
	if t > d*d*d {
		return math.Cbrt(t)
	}
	return t/(3*d*d) + 4.0/29.0
}

// deltaE is CIEDE2000 per Sharma, Wu and Dalal (2005), all weighting factors 1.
// Plain euclidean CIELAB distance rank blues and yellows wrong -- reason floor
// above stated in these units.
func deltaE(p, q [3]float64) float64 {
	l1, a1, b1 := p[0], p[1], p[2]
	l2, a2, b2 := q[0], q[1], q[2]

	c1, c2 := math.Hypot(a1, b1), math.Hypot(a2, b2)
	cb := (c1 + c2) / 2
	g := 0.5 * (1 - math.Sqrt(pow7(cb)/(pow7(cb)+pow7(25))))
	ap1, ap2 := (1+g)*a1, (1+g)*a2
	cp1, cp2 := math.Hypot(ap1, b1), math.Hypot(ap2, b2)
	hp1, hp2 := hue(b1, ap1), hue(b2, ap2)

	dl := l2 - l1
	dc := cp2 - cp1
	var dh float64
	switch {
	case cp1*cp2 == 0:
	case math.Abs(hp2-hp1) <= 180:
		dh = hp2 - hp1
	case hp2-hp1 > 180:
		dh = hp2 - hp1 - 360
	default:
		dh = hp2 - hp1 + 360
	}
	dhp := 2 * math.Sqrt(cp1*cp2) * math.Sin(rad(dh)/2)

	lb, cbp := (l1+l2)/2, (cp1+cp2)/2
	var hb float64
	switch {
	case cp1*cp2 == 0:
		hb = hp1 + hp2
	case math.Abs(hp1-hp2) <= 180:
		hb = (hp1 + hp2) / 2
	case hp1+hp2 < 360:
		hb = (hp1 + hp2 + 360) / 2
	default:
		hb = (hp1 + hp2 - 360) / 2
	}

	tt := 1 - 0.17*math.Cos(rad(hb-30)) + 0.24*math.Cos(rad(2*hb)) +
		0.32*math.Cos(rad(3*hb+6)) - 0.20*math.Cos(rad(4*hb-63))
	sl := 1 + (0.015*(lb-50)*(lb-50))/math.Sqrt(20+(lb-50)*(lb-50))
	sc := 1 + 0.045*cbp
	sh := 1 + 0.015*cbp*tt
	// Rotation term pull blues and purples together, where hue difference read
	// smaller than coordinates say.
	rt := -math.Sin(rad(60*math.Exp(-((hb-275)/25)*((hb-275)/25)))) *
		2 * math.Sqrt(pow7(cbp)/(pow7(cbp)+pow7(25)))

	return math.Sqrt((dl/sl)*(dl/sl) + (dc/sc)*(dc/sc) + (dhp/sh)*(dhp/sh) +
		rt*(dc/sc)*(dhp/sh))
}

// hue return degrees in 0-360. atan2 hand back negatives, mean-hue branches
// above compare against 180 and 360.
func hue(b, ap float64) float64 {
	if b == 0 && ap == 0 {
		return 0
	}
	d := math.Atan2(b, ap) * 180 / math.Pi
	if d < 0 {
		d += 360
	}
	return d
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func pow7(v float64) float64 { return v * v * v * v * v * v * v }
