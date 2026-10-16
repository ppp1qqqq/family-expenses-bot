import { demoApi } from "./demoApi";

export type Expense = {
  id: number;
  user_id: number;
  amount: number;
  category: string;
  spent_at: string;
  comment: string;
  created_at: string;
  updated_at: string;
};

export type User = {
  id: number;
  first_name: string;
  username: string;
};

export type Category = {
  code: string;
  name: string;
};

export type ExpenseList = {
  items: Expense[];
  total: number;
  sum: number;
};

export type Filters = {
  user_id?: string;
  from?: string;
  to?: string;
  category?: string;
  limit: number;
  offset: number;
};

export type ExpenseInput = {
  user_id?: number;
  amount: number;
  category: string;
  spent_at: string;
  comment: string;
};

const BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (resp.status === 204) {
    return undefined as T;
  }
  const body = await resp.json().catch(() => null);
  if (!resp.ok) {
    throw new Error(body?.error?.message ?? `Ошибка ${resp.status}`);
  }
  return body as T;
}

const restApi = {
  listExpenses(f: Filters) {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(f)) {
      if (v !== undefined && v !== "") q.set(k, String(v));
    }
    return request<ExpenseList>(`/expenses?${q}`);
  },
  createExpense(input: ExpenseInput) {
    return request<Expense>("/expenses", { method: "POST", body: JSON.stringify(input) });
  },
  updateExpense(id: number, input: Omit<ExpenseInput, "user_id">) {
    return request<Expense>(`/expenses/${id}`, { method: "PATCH", body: JSON.stringify(input) });
  },
  deleteExpense(id: number) {
    return request<void>(`/expenses/${id}`, { method: "DELETE" });
  },
  users() {
    return request<User[]>("/users");
  },
  categories() {
    return request<Category[]>("/categories");
  },
};

// Сборка для GitHub Pages (VITE_DEMO=1) работает без бэкенда, на данных в браузере.
export const isDemo = import.meta.env.VITE_DEMO === "1";

export const api: typeof restApi = isDemo ? demoApi : restApi;
