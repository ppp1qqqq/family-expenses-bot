// Package domain описывает сущности сервиса и общие для всех слоёв ошибки.
package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateLayout используется везде, где дата траты превращается в строку: API, инструменты агента.
const DateLayout = "2006-01-02"

type Category string

const (
	CategoryFood          Category = "food"
	CategoryTransport     Category = "transport"
	CategoryHousing       Category = "housing"
	CategoryEntertainment Category = "entertainment"
	CategoryHealth        Category = "health"
	CategoryShopping      Category = "shopping"
	CategoryOther         Category = "other"
)

// Categories задаёт порядок категорий в панели и в описании инструментов агента.
var Categories = []Category{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryEntertainment,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

var categoryNames = map[Category]string{
	CategoryFood:          "еда",
	CategoryTransport:     "транспорт",
	CategoryHousing:       "жильё",
	CategoryEntertainment: "развлечения",
	CategoryHealth:        "здоровье",
	CategoryShopping:      "покупки",
	CategoryOther:         "другое",
}

func (c Category) Valid() bool {
	_, ok := categoryNames[c]
	return ok
}

// Name возвращает русское название категории.
func (c Category) Name() string {
	return categoryNames[c]
}

// Expense это одна трата. Сумма хранится в копейках, чтобы не ловить ошибки округления float.
type Expense struct {
	ID        int64
	UserID    int64
	Amount    int64
	Category  Category
	SpentAt   time.Time
	Comment   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User это член семьи, который хотя бы раз написал боту.
type User struct {
	TelegramID int64
	FirstName  string
	Username   string
	CreatedAt  time.Time
}

type SortOrder string

const (
	// SortSpentAt сортирует по дате траты, новые сверху.
	SortSpentAt SortOrder = "spent_at"
	// SortCreatedAt сортирует по времени добавления, последняя добавленная сверху.
	SortCreatedAt SortOrder = "created_at"
)

// ExpenseFilter задаёт выборку трат. Пустые поля не ограничивают выборку.
type ExpenseFilter struct {
	UserID   *int64
	From     *time.Time
	To       *time.Time
	Category *Category
	Text     string
	Sort     SortOrder
	Limit    int
	Offset   int
}

// ExpensePatch содержит только изменяемые поля.
type ExpensePatch struct {
	Amount   *int64
	Category *Category
	SpentAt  *time.Time
	Comment  *string
}

func (p ExpensePatch) Empty() bool {
	return p.Amount == nil && p.Category == nil && p.SpentAt == nil && p.Comment == nil
}

// CategoryTotal это сумма трат по одной категории.
type CategoryTotal struct {
	Category Category
	Amount   int64
	Count    int
}

var ErrNotFound = errors.New("not found")

// ValidationError означает, что входные данные некорректны. Текст показывается пользователю.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func Invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ParseAmount разбирает сумму в рублях («350», «1200.5», «99,90») в копейки.
func ParseAmount(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if strings.HasPrefix(s, "-") {
		return 0, Invalid("сумма должна быть больше нуля")
	}
	rub, kop, hasKop := strings.Cut(s, ".")
	if rub == "" || len(kop) > 2 || (hasKop && kop == "") || !isDigits(rub) || !isDigits(kop) {
		return 0, Invalid("некорректная сумма %q", s)
	}
	r, err := strconv.ParseInt(rub, 10, 64)
	if err != nil || r > 1e12 {
		return 0, Invalid("некорректная сумма %q", s)
	}
	var k int64
	if kop != "" {
		if len(kop) == 1 {
			kop += "0"
		}
		k, _ = strconv.ParseInt(kop, 10, 64)
	}
	return r*100 + k, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FormatAmount печатает копейки как рубли: 35000 -> «350», 35050 -> «350.50».
func FormatAmount(kopecks int64) string {
	if kopecks%100 == 0 {
		return strconv.FormatInt(kopecks/100, 10)
	}
	return fmt.Sprintf("%d.%02d", kopecks/100, kopecks%100)
}

// ParseDate разбирает дату в формате YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(DateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, Invalid("дата %q должна быть в формате ГГГГ-ММ-ДД", s)
	}
	return t, nil
}
