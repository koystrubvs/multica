package handler

import (
	"testing"
	"time"
)

// The purposes below are real payment descriptions from the bank register, kept
// verbatim: the formats vary per payer and per bank, and inventing tidy ones
// would test a string format nobody actually sends.
func TestParseInvoiceReference(t *testing.T) {
	date := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}

	cases := []struct {
		name    string
		purpose string
		number  string
		date    time.Time
	}{
		{
			name:    "dotted date",
			purpose: "Оплата за услуги доработки по сайту по счету № 103 от 20.07.2026 Сумма 26250-00 Без налога (НДС)",
			number:  "103",
			date:    date(2026, time.July, 20),
		},
		{
			name:    "yo in the word and no space before the sign",
			purpose: "Оплата по счёту № 100 от 10.07.2026 за обновление темы, плагинов и ядра сайта Сумма 20000-00",
			number:  "100",
			date:    date(2026, time.July, 10),
		},
		{
			name:    "month spelled out, sign glued to the digits",
			purpose: "Оплата услуг по счету №91 от 30 июня 2026 Сумма 25870-00 Без налога (НДС)",
			number:  "91",
			date:    date(2026, time.June, 30),
		},
		{
			name:    "all caps",
			purpose: "ОПЛАТА ПО СЧЕТУ № 104 ОТ 20 ИЮЛЯ 2026 ЗА РЕДИЗАЙН САЙТА. НДС НЕ ОБЛАГАЕТСЯ",
			number:  "104",
			date:    date(2026, time.July, 20),
		},
		{
			name:    "letter after the year",
			purpose: "ОПЛАТА ПО СЧЕТУ № 97 ОТ 03.07.2026Г. ЗА РАБОТЫ ПО РАЗРАБОТКЕ САЙТА. НДС НЕ ОБЛАГАЕТСЯ",
			number:  "97",
			date:    date(2026, time.July, 3),
		},
		{
			name:    "two-digit year",
			purpose: "Оплата по Счету № 55 от 06.07.26 Поддержка сайтов Сумма 28800-00 Без налога (НДС)",
			number:  "55",
			date:    date(2026, time.July, 6),
		},
		{
			name:    "no sign at all, and a contract number that must not win",
			purpose: "Оплата по Договор №И-1 от 21.12.21 Счет 52 от 06.04.2026 Поддержка сайтов Сумма 28800-00",
			number:  "52",
			date:    date(2026, time.April, 6),
		},
		{
			name:    "comma right after the year",
			purpose: "Оплата по счету 484 от 23.10.23г., за разработку нового личного кабинета Сумма 80000-00",
			number:  "484",
			date:    date(2023, time.October, 23),
		},
		{
			name:    "invoice named without a date still identifies itself",
			purpose: "Оплата по счету № 77 за работы по сайту",
			number:  "77",
		},
		{
			name:    "impossible date is dropped, the number survives",
			purpose: "Оплата по счету № 5 от 31.02.2026 за работы",
			number:  "5",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ref, ok := parseInvoiceReference(testCase.purpose)
			if !ok {
				t.Fatalf("no invoice found in %q", testCase.purpose)
			}
			if ref.Number != testCase.number {
				t.Fatalf("number = %q, want %q", ref.Number, testCase.number)
			}
			if !ref.Date.Equal(testCase.date) {
				t.Fatalf("date = %s, want %s", ref.Date, testCase.date)
			}
		})
	}
}

func TestParseInvoiceReferenceRefuses(t *testing.T) {
	cases := []struct {
		name    string
		purpose string
	}{
		{
			name:    "work described by period, no document named",
			purpose: "За техподдержку сайта тцбум.рф,компьютерных программ за июль 2026 г НДС не облагается",
		},
		{
			name:    "our own transfer note",
			purpose: "TrueSmile — SEO · 2026-07",
		},
		{
			// The word is the same; twenty digits are a settlement account, and
			// reading one as an invoice would attach money to a random line.
			name:    "settlement account",
			purpose: "Перечисление на счет 40702810123456789012 по реестру от 01.07.2026",
		},
		{
			name:    "empty",
			purpose: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if ref, ok := parseInvoiceReference(testCase.purpose); ok {
				t.Fatalf("expected no invoice, got %q", ref.Number)
			}
		})
	}
}

// An account number followed by a real invoice must still resolve to the
// invoice: the guard skips the account rather than giving up on the purpose.
func TestParseInvoiceReferenceSkipsAccountBeforeInvoice(t *testing.T) {
	ref, ok := parseInvoiceReference("Перечисление на счет 40702810123456789012, оплата по счету № 93 от 30 июня 2026")
	if !ok {
		t.Fatal("expected the invoice to be found")
	}
	if ref.Number != "93" {
		t.Fatalf("number = %q, want 93", ref.Number)
	}
	if want := time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC); !ref.Date.Equal(want) {
		t.Fatalf("date = %s, want %s", ref.Date, want)
	}
}

func TestParseInvoiceMonth(t *testing.T) {
	cases := map[string]int{
		"января": 1, "ФЕВРАЛЯ": 2, "марта": 3, "апреля": 4, "мая": 5, "май": 5,
		"июня": 6, "июля": 7, "августа": 8, "сентября": 9, "октября": 10,
		"ноября": 11, "декабря": 12, "07": 7,
	}
	for value, want := range cases {
		got, ok := parseInvoiceMonth(value)
		if !ok || got != want {
			t.Fatalf("parseInvoiceMonth(%q) = %d, %v; want %d", value, got, ok, want)
		}
	}
	for _, value := range []string{"13", "0", "числа", ""} {
		if got, ok := parseInvoiceMonth(value); ok {
			t.Fatalf("parseInvoiceMonth(%q) = %d, want refusal", value, got)
		}
	}
}
