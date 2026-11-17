import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// cn combines shadcn component classes and resolves Tailwind conflicts.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// time formats movie positions without relying on the browser's timezone.
export function time(seconds: number) {
  if (!Number.isFinite(seconds)) {
    return "—";
  }

  const value = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(value / 3600);

  const minutes = String(Math.floor(value / 60) % 60).padStart(2, "0");
  const remainingSeconds = String(value % 60).padStart(2, "0");
  const prefix = hours ? `${hours}:` : "";

  return `${prefix}${minutes}:${remainingSeconds}`;
}

// request returns JSON when present and preserves API error messages.
export async function request<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data =
    response.status === 204 ? null : await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(
      data?.error ||
        (response.status === 403
          ? "Please sign in."
          : `Request failed (${response.status})`),
    );
  }

  return data as T;
}
