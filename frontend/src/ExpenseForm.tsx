import { useState, type FormEvent } from "react";
import { api, type Category, type Expense, type User } from "./api";
import { userName } from "./App";

type Props = {
  expense?: Expense;
  users: User[];
  categories: Category[];
  onClose: () => void;
  onSaved: () => void;
};

function today() {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function ExpenseForm({ expense, users, categories, onClose, onSaved }: Props) {
  const [userId, setUserId] = useState(String(expense?.user_id ?? users[0]?.id ?? ""));
  const [amount, setAmount] = useState(expense ? String(expense.amount) : "");
  const [category, setCategory] = useState(expense?.category ?? categories[0]?.code ?? "");
  const [spentAt, setSpentAt] = useState(expense?.spent_at ?? today());
  const [comment, setComment] = useState(expense?.comment ?? "");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = Number(amount.replace(",", "."));
    if (!Number.isFinite(value) || value <= 0) {
      setError("Сумма должна быть больше нуля");
      return;
    }
    const input = { amount: value, category, spent_at: spentAt, comment };
    setSaving(true);
    try {
      if (expense) {
        await api.updateExpense(expense.id, input);
      } else {
        await api.createExpense({ ...input, user_id: Number(userId) });
      }
      onSaved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="backdrop" onClick={onClose}>
      <form className="modal" onClick={(e) => e.stopPropagation()} onSubmit={submit}>
        <h2>{expense ? "Изменить трату" : "Новая трата"}</h2>

        <label>
          Член семьи
          <select value={userId} onChange={(e) => setUserId(e.target.value)} disabled={!!expense}>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {userName(u, u.id)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Сумма, ₽
          <input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} required autoFocus />
        </label>
        <label>
          Категория
          <select value={category} onChange={(e) => setCategory(e.target.value)}>
            {categories.map((c) => (
              <option key={c.code} value={c.code}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Дата
          <input type="date" value={spentAt} max={today()} onChange={(e) => setSpentAt(e.target.value)} required />
        </label>
        <label>
          Комментарий
          <input value={comment} maxLength={500} onChange={(e) => setComment(e.target.value)} />
        </label>

        {error && <div className="error">{error}</div>}

        <div className="modal-actions">
          <button type="button" onClick={onClose}>
            Отмена
          </button>
          <button type="submit" className="primary" disabled={saving}>
            {saving ? "Сохраняю…" : "Сохранить"}
          </button>
        </div>
      </form>
    </div>
  );
}
