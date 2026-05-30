// domain errors. callers match on these classes, not on message strings.

export class DomainError extends Error {}

export class NotFoundError extends DomainError {
  constructor(id: string) {
    super(`doc not found: ${id}`);
    this.name = "NotFoundError";
  }
}

export class IllegalTransitionError extends DomainError {
  constructor(from: string, to: string) {
    super(`illegal transition: ${from} -> ${to}`);
    this.name = "IllegalTransitionError";
  }
}
