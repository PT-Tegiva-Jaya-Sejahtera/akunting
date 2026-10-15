package templates

// moneyRowLabelClass dan moneyRowValueClass memisahkan gaya baris nilai pada
// kartu dashboard tanpa ekspresi bersyarat di dalam atribut templ.
func moneyRowLabelClass(strong bool) string {
	if strong {
		return "text-sm font-medium text-ink"
	}
	return "text-sm text-label"
}

func moneyRowValueClass(strong bool) string {
	if strong {
		return "num text-sm font-semibold text-ink"
	}
	return "num text-sm text-ink"
}

// kpiValueClass adalah gaya angka besar pada kartu KPI.
func kpiValueClass(accent string) string {
	return "num mt-2 text-2xl font-semibold tracking-tight " + accent
}

// rangeBtnClass adalah gaya tombol filter rentang grafik (aktif atau tidak).
func rangeBtnClass(active bool) string {
	if active {
		return "rounded-[6px] bg-white px-2.5 py-1 text-xs font-semibold text-accent shadow-input"
	}
	return "rounded-[6px] px-2.5 py-1 text-xs font-medium text-muted transition-colors hover:text-ink"
}

// userMenuPanelClass menempatkan popup akun: ke atas di kaki sidebar, ke
// bawah di navbar.
func userMenuPanelClass(placement string) string {
	if placement == "top" {
		return "absolute bottom-full left-0 right-0 z-30 mb-2 rounded-card border border-line bg-white py-1.5 shadow-dropdown"
	}
	return "absolute right-0 z-30 mt-2 w-64 rounded-card border border-line bg-white py-1.5 shadow-dropdown"
}
