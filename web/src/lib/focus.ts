// A small action: put focus on an element when it appears, so a keyboard or screen reader user is taken into it.
export function enter(node: HTMLElement): void {
    queueMicrotask(() => node.focus());
}
