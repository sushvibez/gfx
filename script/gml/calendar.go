package gml

// Qt Quick Controls' calendar pieces: MonthGrid (six weeks of day cells for
// one month) and DayOfWeekRow (the weekday header). Both are delegate views
// over a generated model, so they build like any other view here: the model
// rows are synthesized from the date rather than read from a ListModel.
//
// The locale is accepted and not modeled — weeks start on Sunday and the
// weekday names are English, which is what these pages ask for
// (Qt.locale("en_US")).

import (
	"fmt"
	"time"

	"github.com/sushvibez/gfx/ui"
)

// weekdayShort/Long are DayOfWeekRow's names, Sunday first (Qt.DayOfWeek
// numbering: Sunday is 0).
var weekdayShort = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var weekdayNarrow = []string{"S", "M", "T", "W", "T", "F", "S"}
var weekdayLong = []string{"Sunday", "Monday", "Tuesday", "Wednesday",
	"Thursday", "Friday", "Saturday"}

// buildMonthGrid renders a MonthGrid: 6 rows x 7 columns of day cells for
// `month` (0-based, as in QML) of `year`, including the trailing days of the
// previous month and the leading days of the next one.
func buildMonthGrid(n *node, env *Env, doc *docState) (ui.Element, error) {
	now := time.Now()
	month, year := int(now.Month())-1, now.Year()
	var w, h, spacing float32
	for _, a := range n.attrs {
		switch a.name {
		case "month":
			if f, err := attrFloat(a, env); err == nil {
				month = int(f)
			}
		case "year":
			if f, err := attrFloat(a, env); err == nil {
				year = int(f)
			}
		case "spacing":
			if f, err := attrFloat(a, env); err == nil {
				spacing = f
			}
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "delegate", "id", "locale", "title", "clip", "z", "visible",
			"opacity", "enabled", "padding", "topPadding", "bottomPadding",
			"leftPadding", "rightPadding", "background", "contentItem",
			"anchors.fill", "anchors.margins", "anchors.centerIn":
			// accepted, applied below or not modeled
		case "onClicked", "onPressed", "onReleased":
			// cell signals: the delegates in this corpus carry their own
			// MouseArea, which does the same job.
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: MonthGrid has no attribute %q", a.line, a.name)
		}
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	grid := ui.Div()
	if w > 0 {
		grid.W(w)
	}
	if h > 0 {
		grid.H(h)
	}
	if spacing > 0 {
		grid.Gap(spacing)
	}
	// The grid starts on the Sunday of the week containing the 1st, and is
	// always 42 cells — QML's MonthGrid is a fixed 6x7 so the calendar does
	// not change height from month to month.
	// The grid's own properties, which its delegates read back (cell width
	// from monthGrid.width - 6 * monthGrid.spacing, and the month each cell
	// compares against to grey out the adjacent months).
	doc.exposeIDProp(n, "month", float64(month))
	doc.exposeIDProp(n, "year", float64(year))
	doc.exposeIDProp(n, "spacing", float64(spacing))
	cells := monthCells(month, year, time.Now())
	viewKey := doc.key(n)
	for week := 0; week < 6; week++ {
		row := ui.Div().Row()
		if spacing > 0 {
			row.Gap(spacing)
		}
		for col := 0; col < 7; col++ {
			i := week*7 + col
			cell := cells[i]
			restore := enterRow(viewKey, delegate, env, doc, cell, i)
			el, err := build(delegate, env, doc)
			restore()
			if err != nil {
				return nil, err
			}
			row.Children(el)
		}
		grid.Children(row)
	}
	return grid, nil
}

// buildDayOfWeekRow renders a DayOfWeekRow: seven weekday-name cells.
func buildDayOfWeekRow(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h, spacing float32
	for _, a := range n.attrs {
		switch a.name {
		case "spacing":
			if f, err := attrFloat(a, env); err == nil {
				spacing = f
			}
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "delegate", "id", "locale", "clip", "z", "visible", "opacity",
			"enabled", "padding", "topPadding", "bottomPadding",
			"leftPadding", "rightPadding", "background", "contentItem",
			"anchors.fill", "anchors.margins", "anchors.centerIn":
			// accepted, applied below or not modeled
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: DayOfWeekRow has no attribute %q", a.line, a.name)
		}
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	row := ui.Div().Row()
	if w > 0 {
		row.W(w)
	}
	if h > 0 {
		row.H(h)
	}
	if spacing > 0 {
		row.Gap(spacing)
	}
	viewKey := doc.key(n)
	for i, cell := range dayOfWeekCells() {
		restore := enterRow(viewKey, delegate, env, doc, cell, i)
		el, err := build(delegate, env, doc)
		restore()
		if err != nil {
			return nil, err
		}
		row.Children(el)
	}
	return row, nil
}

// monthCells is MonthGrid's model: 42 day cells starting on the Sunday of
// the week containing the 1st, so the calendar keeps one height all year.
// Days from the adjacent months carry their own month, which is how a
// delegate greys them out.
func monthCells(month, year int, today time.Time) []map[string]any {
	first := time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
	start := first.AddDate(0, 0, -int(first.Weekday()))
	cells := make([]map[string]any, 42)
	for i := range cells {
		d := start.AddDate(0, 0, i)
		cells[i] = map[string]any{
			"day":     float64(d.Day()),
			"month":   float64(int(d.Month()) - 1), // QML months are 0-based
			"year":    float64(d.Year()),
			"weekDay": float64(int(d.Weekday())),
			"today":   sameDay(d, today),
			"date":    dateObject(d),
		}
	}
	return cells
}

// dayOfWeekCells is DayOfWeekRow's model: the seven weekday names, Sunday
// first (Qt.DayOfWeek numbering).
func dayOfWeekCells() []map[string]any {
	cells := make([]map[string]any, 7)
	for i := range cells {
		cells[i] = map[string]any{
			"day":        float64(i),
			"shortName":  weekdayShort[i],
			"narrowName": weekdayNarrow[i],
			"longName":   weekdayLong[i],
			"modelData":  weekdayShort[i],
		}
	}
	return cells
}

// sameDay reports whether two times fall on the same calendar day.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
