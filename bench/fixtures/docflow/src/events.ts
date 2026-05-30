// minimal typed event bus. the service emits a DocEvent on every state change
// so other parts of the system can react without polling the repository.

import type { DocStatus } from "./types.ts";

export interface DocEvent {
  type: DocStatus | "created";
  id: string;
  at: number;
}

type Handler = (e: DocEvent) => void;

export class EventBus {
  private handlers: Handler[] = [];

  on(h: Handler): void {
    this.handlers.push(h);
  }

  emit(e: DocEvent): void {
    for (const h of this.handlers) h(e);
  }
}
