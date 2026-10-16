// Демо-режим для GitHub Pages: тот же интерфейс, что у REST API, но данные живут в браузере.
// Включается при сборке с VITE_DEMO=1. Правила повторяют бэкенд: сумма больше нуля, дата не в будущем.
import type { Category, Expense, ExpenseInput, ExpenseList, Filters, User } from "./api";

const STORAGE_KEY = "family-expenses-demo-v1";

const categories: Category[] = [
  { code: "food", name: "еда" },
  { code: "transport", name: "транспорт" },
  { code: "housing", name: "жильё" },
  { code: "entertainment", name: "развлечения" },
  { code: "health", name: "здоровье" },
  { code: "shopping", name: "покупки" },
  { code: "other", name: "другое" },
];

const users: User[] = [
  { id: 101, first_name: "Анна", username: "anna" },
  { id: 102, first_name: "Сергей", username: "" },
  { id: 103, first_name: "Маша", username: "masha" },
];

// Типичные траты: категория, комментарий, диапазон суммы в рублях.
const templates: [string, string, number, number][] = [
  ["food", "кофе", 180, 450],
  ["food", "продукты", 900, 4500],
  ["food", "обед", 350, 900],
  ["food", "доставка еды", 800, 2500],
  ["transport", "такси", 300, 1500],
  ["transport", "метро", 60, 60],
  ["transport", "бензин", 2000, 4000],
  ["housing", "коммуналка", 6000, 9000],
  ["housing", "интернет", 700, 700],
  ["entertainment", "кино", 500, 1200],
  ["entertainment", "подписка", 299, 699],
  ["health", "аптека", 250, 2200],
  ["shopping", "одежда", 1500, 7000],
  ["shopping", "товары для дома", 400, 2500],
  ["other", "подарок", 1000, 5000],
];

type State = { nextId: number; expenses: Expense[] };

function pad(n: number) {
  return String(n).padStart(2, "0");
}

function isoDate(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// Детерминированный генератор, чтобы демо выглядело одинаково у всех.
function mulberry32(seed: number) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function seed(): State {
  const rand = mulberry32(42);
  const expenses: Expense[] = [];
  const today = new Date();
  for (let daysAgo = 59; daysAgo >= 0; daysAgo--) {
    const day = new Date(today.getFullYear(), today.getMonth(), today.getDate() - daysAgo);
    const count = Math.floor(rand() * 3);
    for (let i = 0; i < count; i++) {
      const [category, comment, lo, hi] = templates[Math.floor(rand() * templates.length)];
      const amount = lo === hi ? lo : Math.round((lo + rand() * (hi - lo)) / 10) * 10;
      const user = users[Math.floor(rand() * users.length)];
      const created = new Date(day.getTime() + (9 + i * 4) * 3600_000).toISOString();
      expenses.push({
        id: expenses.length + 1,
        user_id: user.id,
        amount,
        category,
        spent_at: isoDate(day),
        comment,
        created_at: created,
        updated_at: created,
      });
    }
  }
  return { nextId: expenses.length + 1, expenses };
}

// Данные создаются при первом обращении, чтобы обычная сборка с бэкендом их не трогала.
let cached: State | null = null;

function db(): State {
  cached ??= load();
  return cached;
}

function load(): State {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) return JSON.parse(raw) as State;
  } catch {
    // Хранилище недоступно (приватный режим): работаем с данными в памяти.
  }
  return seed();
}

function save() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(db()));
  } catch {
    // Без хранилища изменения проживут до перезагрузки страницы.
  }
}

function validate(amount: number, category: string, spentAt: string) {
  if (!(amount > 0)) throw new Error("сумма должна быть больше нуля");
  if (Math.round(amount * 100) !== amount * 100) throw new Error("у суммы не больше двух знаков после запятой");
  if (!categories.some((c) => c.code === category)) throw new Error(`неизвестная категория "${category}"`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(spentAt)) throw new Error("дата должна быть в формате ГГГГ-ММ-ДД");
  if (spentAt > isoDate(new Date())) throw new Error(`дата траты ${spentAt} ещё не наступила`);
}

function find(id: number) {
  const e = db().expenses.find((x) => x.id === id);
  if (!e) throw new Error("трата не найдена");
  return e;
}

const delay = <T>(v: T) => new Promise<T>((resolve) => setTimeout(() => resolve(v), 120));

export const demoApi = {
  async listExpenses(f: Filters): Promise<ExpenseList> {
    const items = db().expenses
      .filter(
        (e) =>
          (!f.user_id || e.user_id === Number(f.user_id)) &&
          (!f.from || e.spent_at >= f.from) &&
          (!f.to || e.spent_at <= f.to) &&
          (!f.category || e.category === f.category),
      )
      .sort((a, b) => b.spent_at.localeCompare(a.spent_at) || b.created_at.localeCompare(a.created_at));
    const sum = Math.round(items.reduce((s, e) => s + e.amount * 100, 0)) / 100;
    return delay({ items: items.slice(f.offset, f.offset + f.limit), total: items.length, sum });
  },

  async createExpense(input: ExpenseInput): Promise<Expense> {
    validate(input.amount, input.category, input.spent_at);
    if (!users.some((u) => u.id === input.user_id)) throw new Error(`пользователь ${input.user_id} не найден`);
    const now = new Date().toISOString();
    const e: Expense = {
      id: db().nextId++,
      user_id: input.user_id!,
      amount: input.amount,
      category: input.category,
      spent_at: input.spent_at,
      comment: input.comment.trim(),
      created_at: now,
      updated_at: now,
    };
    db().expenses.push(e);
    save();
    return delay(e);
  },

  async updateExpense(id: number, input: Omit<ExpenseInput, "user_id">): Promise<Expense> {
    const e = find(id);
    validate(input.amount, input.category, input.spent_at);
    Object.assign(e, { ...input, comment: input.comment.trim(), updated_at: new Date().toISOString() });
    save();
    return delay(e);
  },

  async deleteExpense(id: number): Promise<void> {
    find(id);
    db().expenses = db().expenses.filter((e) => e.id !== id);
    save();
    return delay(undefined);
  },

  async users() {
    return delay(users);
  },

  async categories() {
    return delay(categories);
  },
};

export function resetDemo() {
  cached = seed();
  save();
}
