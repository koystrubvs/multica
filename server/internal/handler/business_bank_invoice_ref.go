package handler

// business_bank_invoice_ref.go — reading the invoice a payer names in the
// payment purpose.
//
// Every invoice we issue is an Elba document with a number printed on it, and
// payers quote that number back: "Оплата по счету № 93 от 30 июня 2026". The
// document id we store is a UUID and never reaches a bank statement, so the
// number is the only exact link between an incoming payment and a plan line.
// Seven of every ten inbound client payments on record carry one.

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// invoiceReference is an invoice named in a payment purpose.
type invoiceReference struct {
	Number string
	Date   time.Time // zero when the purpose names no date
}

// invoiceNumberMaxDigits keeps a settlement account out of the parse: "на счет
// 40702810..." is where the money went, not what it paid for. Real invoice
// numbers run two to four digits.
const invoiceNumberMaxDigits = 6

var (
	// Anchored on the word "счёт" rather than on "№", because purposes name
	// other numbered things — "Оплата по Договор №И-1 от 21.12.21 Счет 52 от
	// 06.04.2026" — and because the sign before the digits is often missing.
	invoiceNumberRe = regexp.MustCompile(`(?i)сч[ёе]т[а-яё]*\s*(?:№|#|no|n)?\s*(\d+)`)
	// The issue date trails the number as "от <дата>": dotted, or with the
	// month spelled out, and the year is often two digits.
	invoiceDateRe = regexp.MustCompile(`(?i)^\s*г?\.?\s*от\s+(\d{1,2})\s*[.\s/-]\s*([а-яё]+|\d{1,2})\s*[.\s/-]?\s*(\d{4}|\d{2})`)
	// Ordered so that a three-letter probe resolves before the two-letter one:
	// "марта" must not fall through to May.
	invoiceMonthPrefixes = []string{"янв", "фев", "мар", "апр", "ма", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
)

// parseInvoiceReference pulls the first invoice the purpose names. A purpose
// that names none — recurring support billed by description, or a transfer
// with a free-form note — yields nothing, and the payment falls back to being
// matched by date and amount.
func parseInvoiceReference(purpose string) (invoiceReference, bool) {
	for _, match := range invoiceNumberRe.FindAllStringSubmatchIndex(purpose, -1) {
		digits := purpose[match[2]:match[3]]
		if len(digits) > invoiceNumberMaxDigits {
			// An account number wearing the same word; keep looking, the
			// invoice may still be named further along.
			continue
		}
		number := strings.TrimLeft(digits, "0")
		if number == "" {
			continue
		}
		return invoiceReference{
			Number: number,
			Date:   parseInvoiceDate(purpose[match[1]:]),
		}, true
	}
	return invoiceReference{}, false
}

func parseInvoiceDate(rest string) time.Time {
	match := invoiceDateRe.FindStringSubmatch(rest)
	if match == nil {
		return time.Time{}
	}
	day, err := strconv.Atoi(match[1])
	if err != nil {
		return time.Time{}
	}
	month, ok := parseInvoiceMonth(match[2])
	if !ok {
		return time.Time{}
	}
	year, err := strconv.Atoi(match[3])
	if err != nil {
		return time.Time{}
	}
	if year < 100 {
		year += 2000
	}
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	// Reject what only looks like a date: Go rolls 31 February forward instead
	// of failing, and a rolled date would match the wrong invoice.
	if parsed.Day() != day || int(parsed.Month()) != month {
		return time.Time{}
	}
	return parsed
}

func parseInvoiceMonth(value string) (int, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if numeric, err := strconv.Atoi(value); err == nil {
		if numeric < 1 || numeric > 12 {
			return 0, false
		}
		return numeric, true
	}
	for index, prefix := range invoiceMonthPrefixes {
		if strings.HasPrefix(value, prefix) {
			return index + 1, true
		}
	}
	return 0, false
}
