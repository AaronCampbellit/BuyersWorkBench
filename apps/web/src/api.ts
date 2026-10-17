export type User = {
  id: string;
  display_name: string;
  email: string;
  role: "owner" | "reviewer";
  must_change_password?: boolean;
};

export type Session = { user: User; csrf_token: string };

export type Workspace = {
  id: string;
  name: string;
  kind: "private" | "presentation" | "reviewer";
  generation: number;
  seed_version: string | null;
  business_date: string | null;
  data_origin: "fictional" | "no_supplier_data";
};

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export function isAborted(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "fictional_workspace_required")
      return "This operation requires a presentation or reviewer workspace.";
    return error.message;
  }
  return "Could not reach the local API. Check that the API is running, then retry.";
}

export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(path, {
    ...options,
    credentials: "same-origin",
  });
  if (!response.ok) {
    let message = "The request could not be completed. Please retry.";
    let code: string | undefined;
    try {
      const body = (await response.json()) as { error?: string; code?: string };
      if (typeof body.error === "string") message = body.error;
      if (typeof body.code === "string") code = body.code;
    } catch {
      // A proxy or unavailable API may return a response without a JSON body.
    }
    throw new ApiError(response.status, message, code);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}
