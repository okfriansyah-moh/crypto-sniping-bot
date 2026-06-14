/**
 * Operator command API — POST /api/v1/commands and /api/v1/commands/confirm.
 * Mirrors backend-dashboard/internal/api/commands/types.go.
 */

import { apiPost } from "./client";

export type CommandType = "mode" | "kill" | "resume" | "force_close";

export interface CommandSubmitRequest {
  command_type: CommandType;
  issuer_id: string;
  args?: Record<string, string>;
}

export interface CommandConfirmRequest {
  confirm_token: string;
  issuer_id: string;
  command_type: CommandType;
  args?: Record<string, string>;
}

export interface CommandResponse {
  status: "accepted" | "confirmation_required";
  command_id?: string;
  confirm_token?: string;
  expires_at?: string;
}

export function submitCommand(body: CommandSubmitRequest): Promise<CommandResponse> {
  return apiPost<CommandResponse>("/api/v1/commands", body);
}

export function confirmCommand(body: CommandConfirmRequest): Promise<CommandResponse> {
  return apiPost<CommandResponse>("/api/v1/commands/confirm", body);
}

/** Mode change — non-destructive, single POST. */
export async function submitModeChange(
  mode: string,
  issuerId: string,
): Promise<CommandResponse> {
  return submitCommand({
    command_type: "mode",
    issuer_id: issuerId,
    args: { mode },
  });
}

/**
 * Kill or resume — backend returns confirmation_required; auto-confirms with token.
 */
export async function submitDestructiveCommand(
  commandType: "kill" | "resume",
  issuerId: string,
): Promise<CommandResponse> {
  const challenge = await submitCommand({
    command_type: commandType,
    issuer_id: issuerId,
  });
  if (
    challenge.status === "confirmation_required" &&
    challenge.confirm_token
  ) {
    return confirmCommand({
      confirm_token: challenge.confirm_token,
      issuer_id: issuerId,
      command_type: commandType,
    });
  }
  return challenge;
}
