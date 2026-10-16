import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Category, type Expense, type ExpenseList, type Filters, type User } from "./api";
import { ExpenseForm } from "./ExpenseForm";

const PAGE_SIZE = 50;

const money = new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 2 });
const dateFmt = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" });

export function formatDate(iso: string) {
  const [y, m, d] = iso.split("-").map(Number);
  return dateFmt.format(new Date(y, m - 1, d));
}

export function userName(u: User | undefined, id: number) {
  if (!u) return `ID ${id}`;
  return u.username ? `${u.first_name} (@${u.username})` : u.first_name;
}

type Editing = { mode: "create" } | { mode: "edit"; expense: Expense } | null;

export function App() {
  const [users, setUsers] = useState<User[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [filters, setFilters] = useState<Filters>({ limit: PAGE_SIZE, offset: 0 });
  const [data, setData] = useState<ExpenseList | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Editing>(null);

  const usersById = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const categoryName = useMemo(() => new Map(categories.map((c) => [c.code, c.name])), [categories]);

  const loadDictionaries = useCallback(() => {
    Promise.all([api.users(), api.categories()])
      .then(([u, c]) => {
        setUsers(u);
        setCategories(c);
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  const loadExpenses = useCallback(() => {
    setLoading(true);
    api
      .listExpenses(filters)
      .then((d) => {
        setData(d);
        setError("");
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [filters]);

  useEffect(loadDictionaries, [loadDictionaries]);
  useEffect(loadExpenses, [loadExpenses]);

  const setFilter = (key: keyof Filters, value: string) =>
    setFilters((f) => ({ ...f, [key]: value || undefined, offset: 0 }));

  const remove = async (e: Expense) => {
    const what = e.comment || categoryName.get(e.category) || "трату";
    if (!confirm(`Удалить «${what}» на ${money.format(e.amount)} от ${formatDate(e.spent_at)}?`)) return;
    try {
      await api.deleteExpense(e.id);
      loadExpenses();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const onSaved = () => {
    setEditing(null);
    loadDictionaries();
    loadExpenses();
  };

  const page = Math.floor(filters.offset / PAGE_SIZE) + 1;
  const pages = data ? Math.max(1, Math.ceil(data.total / PAGE_SIZE)) : 1;

  return (
    <div className="page">
      <header className="header">
        <h1>Расходы семьи</h1>
        <button className="primary" onClick={() => setEditing({ mode: "create" })} disabled={users.length === 0}>
          Добавить трату
        </button>
      </header>

      <section className="filters">
        <label>
          Член семьи
          <select value={filters.user_id ?? ""} onChange={(e) => setFilter("user_id", e.target.value)}>
            <option value="">Все</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {userName(u, u.id)}
              </option>
            ))}
          </select>
        </label>
        <label>
          С
          <input type="date" value={filters.from ?? ""} onChange={(e) => setFilter("from", e.target.value)} />
        </label>
        <label>
          По
          <input type="date" value={filters.to ?? ""} onChange={(e) => setFilter("to", e.target.value)} />
        </label>
        <label>
          Категория
          <select value={filters.category ?? ""} onChange={(e) => setFilter("category", e.target.value)}>
            <option value="">Все</option>
            {categories.map((c) => (
              <option key={c.code} value={c.code}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        <button onClick={() => setFilters({ limit: PAGE_SIZE, offset: 0 })}>Сбросить</button>
      </section>

      {error && <div className="error">{error}</div>}

      {data && (
        <p className="summary">
          {data.total === 0 ? "Трат не найдено" : `Трат: ${data.total}, на сумму ${money.format(data.sum)}`}
          {loading && <span className="muted"> · обновляется…</span>}
        </p>
      )}

      {users.length === 0 && !error && (
        <p className="muted">
          Пока никто не писал боту. Член семьи появится здесь после первого сообщения боту в Telegram.
        </p>
      )}

      {data && data.items.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Дата</th>
                <th>Член семьи</th>
                <th>Категория</th>
                <th className="num">Сумма</th>
                <th>Комментарий</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {data.items.map((e) => (
                <tr key={e.id}>
                  <td>{formatDate(e.spent_at)}</td>
                  <td>{userName(usersById.get(e.user_id), e.user_id)}</td>
                  <td>{categoryName.get(e.category) ?? e.category}</td>
                  <td className="num">{money.format(e.amount)}</td>
                  <td>{e.comment}</td>
                  <td className="actions">
                    <button onClick={() => setEditing({ mode: "edit", expense: e })}>Изменить</button>
                    <button className="danger" onClick={() => remove(e)}>
                      Удалить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {pages > 1 && (
        <nav className="pager">
          <button disabled={page <= 1} onClick={() => setFilters((f) => ({ ...f, offset: f.offset - PAGE_SIZE }))}>
            Назад
          </button>
          <span>
            {page} из {pages}
          </span>
          <button disabled={page >= pages} onClick={() => setFilters((f) => ({ ...f, offset: f.offset + PAGE_SIZE }))}>
            Вперёд
          </button>
        </nav>
      )}

      {editing && (
        <ExpenseForm
          expense={editing.mode === "edit" ? editing.expense : undefined}
          users={users}
          categories={categories}
          onClose={() => setEditing(null)}
          onSaved={onSaved}
        />
      )}
    </div>
  );
}
