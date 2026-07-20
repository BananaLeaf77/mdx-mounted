// service/invoice.go
package service

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"chronosphere/domain"

	"github.com/go-pdf/fpdf"
)

// formatRupiah formats a float amount into Indonesian thousand-separator
// style, e.g. 1200000 -> "Rp1.200.000"
func formatRupiah(amount float64) string {
	n := int64(amount)
	s := strconv.FormatInt(n, 10)

	neg := false
	if n < 0 {
		neg = true
		s = s[1:]
	}

	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	result := strings.Join(parts, ".")

	if neg {
		result = "-" + result
	}
	return "Rp " + result
}

// drawLogo places the MDX brand logo image inside the given square area.
func drawLogo(pdf *fpdf.Fpdf, x, y, size float64) {
	pdf.ImageOptions("invoice_logo.jpg", x, y, size, size, false, fpdf.ImageOptions{
		ImageType: "",
		ReadDpi:   true,
	}, 0, "")
}

// statusBadge draws a small rounded, filled pill with the payment status.
func statusBadge(pdf *fpdf.Fpdf, rightX, y float64, status string) {
	label := strings.ToUpper(status)
	if label == "" {
		label = "PENDING"
	}

	var fill [3]int
	switch label {
	case "PAID", "CONFIRMED":
		fill = [3]int{39, 174, 96}
	case "EXPIRED":
		fill = [3]int{192, 57, 43}
	case "REJECTED":
		fill = [3]int{192, 57, 43}
	default:
		fill = [3]int{230, 160, 20}
	}

	pdf.SetFont("Arial", "B", 9)
	w := pdf.GetStringWidth(label) + 8
	x := rightX - w

	pdf.SetFillColor(fill[0], fill[1], fill[2])
	pdf.RoundedRect(x, y, w, 6.5, 3.2, "1234", "F")
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(x, y)
	pdf.CellFormat(w, 6.5, label, "", 0, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
}

func GenerateInvoicePDF(payment *domain.Payment, student *domain.User, pkg *domain.Package, adminTelephone string, registrationFee float64) ([]byte, error) {
	const (
		companyName = "MDX Music Course"
		companyAddr = "Jl. Sedap Malam, No. 400, Kesiman, Denpasar Timur"
	)

	orange := [3]int{237, 121, 16}
	peach := [3]int{253, 243, 233}
	grayText := [3]int{110, 110, 110}
	borderGray := [3]int{225, 225, 225}
	red := [3]int{192, 57, 43}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetMargins(15, 15, 15)
	pageW, _ := pdf.GetPageSize()
	rightEdge := pageW - 15

	// ── Header: logo + company info (left) ─────────────────────────────
	drawLogo(pdf, 15, 15, 16)
	if pdf.Err() {
		log.Printf("fpdf error after logo: %v", pdf.Error())
	}

	pdf.SetXY(15, 34)
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(orange[0], orange[1], orange[2])
	pdf.CellFormat(100, 5.5, companyName, "", 2, "L", false, 0, "")

	pdf.SetX(15)
	pdf.SetFont("Arial", "", 8.5)
	pdf.SetTextColor(grayText[0], grayText[1], grayText[2])
	pdf.MultiCell(100, 4, companyAddr, "", "L", false)
	if adminTelephone != "" {
		pdf.SetX(15)
		pdf.CellFormat(100, 4, "Telp. 0821-3106-681", "", 1, "L", false, 0, "")
	}
	pdf.SetTextColor(0, 0, 0)
	leftBottomY := pdf.GetY()

	// ── Header: INVOICE title + meta (right) ───────────────────────────
	pdf.SetXY(rightEdge-90, 15)
	pdf.SetFont("Arial", "B", 24)
	pdf.SetTextColor(orange[0], orange[1], orange[2])
	pdf.CellFormat(90, 10, "INVOICE", "", 2, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	invoiceDate := time.Now()
	if payment.PaidAt != nil {
		invoiceDate = *payment.PaidAt
	}

	pdf.SetX(rightEdge - 90)
	pdf.SetFont("Arial", "", 9.5)
	pdf.SetTextColor(grayText[0], grayText[1], grayText[2])
	pdf.CellFormat(90, 5, fmt.Sprintf("No: %s", payment.ExternalID), "", 2, "R", false, 0, "")
	pdf.SetX(rightEdge - 90)
	pdf.CellFormat(90, 5, invoiceDate.Format("2 Jan 2006"), "", 1, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	badgeY := pdf.GetY() + 2
	statusBadge(pdf, rightEdge, badgeY, payment.Status)
	rightBottomY := badgeY + 6.5

	// ── Divider ─────────────────────────────────────────────────────────
	dividerY := leftBottomY
	if rightBottomY > dividerY {
		dividerY = rightBottomY
	}
	pdf.SetY(dividerY + 3)
	pdf.SetDrawColor(borderGray[0], borderGray[1], borderGray[2])
	pdf.SetLineWidth(0.3)
	pdf.Line(15, pdf.GetY(), rightEdge, pdf.GetY())
	pdf.Ln(6)

	// ── Customer info box (height grows/shrinks to fit its content) ────
	boxY := pdf.GetY()
	paymentMethod := "-"
	if payment.PaymentMethod != nil && *payment.PaymentMethod != "" {
		paymentMethod = *payment.PaymentMethod
	}

	const (
		boxPadTop    = 4.0
		boxPadBottom = 4.0
		lineH        = 5.0
	)
	boxInnerW := (rightEdge - 15) - 10 // 5mm padding on each side inside the box

	// Measure how many lines each field will actually take, so the box
	// can be sized to fit before anything is drawn.
	pdf.SetFont("Arial", "", 9.5)
	countLines := func(s string) int {
		if s == "" {
			return 0
		}
		lines := pdf.SplitLines([]byte(s), boxInnerW)
		if len(lines) == 0 {
			return 1
		}
		return len(lines)
	}
	nameLines := countLines(student.Name)
	if nameLines == 0 {
		nameLines = 1
	}
	emailLines := countLines(student.Email)
	if emailLines == 0 {
		emailLines = 1
	}
	phoneLines := countLines(student.Phone)
	methodLines := 0
	if paymentMethod != "-" {
		methodLines = 1
	}

	totalLines := 1 + nameLines + emailLines + phoneLines + methodLines // +1 for the "Customer" label
	boxH := boxPadTop + boxPadBottom + float64(totalLines)*lineH

	pdf.SetFillColor(peach[0], peach[1], peach[2])
	pdf.Rect(15, boxY, rightEdge-15, boxH, "F")

	pdf.SetXY(20, boxY+boxPadTop)
	pdf.SetFont("Arial", "B", 10)
	pdf.SetTextColor(orange[0], orange[1], orange[2])
	pdf.CellFormat(0, lineH, "Customer", "", 2, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	pdf.SetX(20)
	pdf.SetFont("Arial", "", 9.5)
	pdf.MultiCell(boxInnerW, lineH, student.Name, "", "L", false)

	pdf.SetX(20)
	pdf.MultiCell(boxInnerW, lineH, student.Email, "", "L", false)

	if student.Phone != "" {
		pdf.SetX(20)
		pdf.MultiCell(boxInnerW, lineH, student.Phone, "", "L", false)
	}

	if paymentMethod != "-" {
		pdf.SetX(20)
		pdf.SetFont("Arial", "B", 9.5)
		pdf.CellFormat(0, lineH, paymentMethod, "", 1, "L", false, 0, "")
	}

	pdf.SetY(boxY + boxH + 8)

	// ── Table header ─────────────────────────────────────────────────────
	tableW := rightEdge - 15
	colDesc := tableW * 0.42
	colQty := tableW * 0.14
	colCost := tableW * 0.22
	colAmt := tableW - colDesc - colQty - colCost

	pdf.SetFillColor(orange[0], orange[1], orange[2])
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 9.5)
	pdf.SetX(15)
	pdf.CellFormat(colDesc, 8.5, "  Deskripsi", "", 0, "L", true, 0, "")
	pdf.CellFormat(colQty, 8.5, "Qty", "", 0, "C", true, 0, "")
	pdf.CellFormat(colCost, 8.5, "Harga", "", 0, "L", true, 0, "")
	pdf.CellFormat(colAmt, 8.5, "Subtotal", "", 1, "L", true, 0, "")
	pdf.SetTextColor(0, 0, 0)

	// ── Table rows ────────────────────────────────────────────────────────
	itemPrice := pkg.Price
	if pkg.IsPromoActive && pkg.PromoPrice > 0 {
		itemPrice = pkg.PromoPrice
	}

	// Package row
	pdf.SetFont("Arial", "", 9.5)
	pdf.SetX(15)
	pdf.CellFormat(colDesc, 9, "  "+pkg.Name, "", 0, "L", false, 0, "")
	pdf.CellFormat(colQty, 9, "1", "", 0, "C", false, 0, "")
	pdf.CellFormat(colCost, 9, formatRupiah(itemPrice), "", 0, "L", false, 0, "")
	pdf.CellFormat(colAmt, 9, formatRupiah(itemPrice), "", 1, "L", false, 0, "")

	// Registration fee row (only when > 0)
	if registrationFee > 0 {
		pdf.SetFont("Arial", "", 9.5)
		pdf.SetX(15)
		pdf.CellFormat(colDesc, 9, "  Biaya Pendaftaran", "", 0, "L", false, 0, "")
		pdf.CellFormat(colQty, 9, "1", "", 0, "C", false, 0, "")
		pdf.CellFormat(colCost, 9, formatRupiah(registrationFee), "", 0, "L", false, 0, "")
		pdf.CellFormat(colAmt, 9, formatRupiah(registrationFee), "", 1, "L", false, 0, "")
	}

	pdf.SetDrawColor(borderGray[0], borderGray[1], borderGray[2])
	pdf.Line(15, pdf.GetY(), rightEdge, pdf.GetY())
	pdf.Ln(6)

	// ── Totals ────────────────────────────────────────────────────────────
	labelW, valueW := 40.0, 40.0
	labelX := rightEdge - labelW - valueW
	valueX := rightEdge - valueW

	// Show discount breakdown when promo is active
	if pkg.IsPromoActive && pkg.PromoPrice > 0 && pkg.PromoPrice < pkg.Price {
		pdf.SetFont("Arial", "", 10)
		pdf.SetXY(labelX, pdf.GetY())
		pdf.CellFormat(labelW, 6.5, "Harga Normal", "", 0, "L", false, 0, "")
		pdf.SetXY(valueX, pdf.GetY())
		pdf.CellFormat(valueW, 6.5, formatRupiah(pkg.Price), "", 1, "R", false, 0, "")

		discount := pkg.Price - pkg.PromoPrice
		pdf.SetFont("Arial", "", 10)
		pdf.SetXY(labelX, pdf.GetY())
		pdf.CellFormat(labelW, 6.5, "Diskon", "", 0, "L", false, 0, "")
		pdf.SetXY(valueX, pdf.GetY())
		pdf.SetTextColor(red[0], red[1], red[2])
		pdf.CellFormat(valueW, 6.5, "-"+formatRupiah(discount), "", 1, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}

	// Subtotal (package price after discount)
	pdf.SetFont("Arial", "", 10)
	pdf.SetXY(labelX, pdf.GetY())
	pdf.CellFormat(labelW, 6.5, "Subtotal", "", 0, "L", false, 0, "")
	pdf.SetXY(valueX, pdf.GetY())
	pdf.CellFormat(valueW, 6.5, formatRupiah(itemPrice), "", 1, "R", false, 0, "")

	// Registration fee in totals (only when > 0)
	if registrationFee > 0 {
		pdf.SetFont("Arial", "", 10)
		pdf.SetXY(labelX, pdf.GetY())
		pdf.CellFormat(labelW, 6.5, "Registrasi", "", 0, "L", false, 0, "")
		pdf.SetXY(valueX, pdf.GetY())
		pdf.CellFormat(valueW, 6.5, "+"+formatRupiah(registrationFee), "", 1, "R", false, 0, "")
	}

	pdf.SetDrawColor(orange[0], orange[1], orange[2])
	pdf.SetLineWidth(0.4)
	pdf.Line(labelX, pdf.GetY(), rightEdge, pdf.GetY())
	pdf.Ln(2)

	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(orange[0], orange[1], orange[2])
	pdf.SetXY(labelX, pdf.GetY())
	pdf.CellFormat(labelW, 7, "Total", "", 0, "L", false, 0, "")
	pdf.SetXY(valueX, pdf.GetY())
	pdf.CellFormat(valueW, 7, formatRupiah(payment.Amount), "", 1, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	// ── Footer ────────────────────────────────────────────────────────────
	pdf.SetY(-40)
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(grayText[0], grayText[1], grayText[2])
	pdf.CellFormat(0, 5, "Terima kasih telah mempercayakan layanan kami!", "", 1, "C", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
