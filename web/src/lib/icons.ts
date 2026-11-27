// Icons as data. Each is drawn by Icon.svelte from these shapes, so no markup is built from a string.
export type Shape =
    | { t: 'path'; d: string; fill?: boolean }
    | { t: 'circle'; cx: number; cy: number; r: number; dash?: string; fill?: boolean }
    | { t: 'rect'; x: number; y: number; w: number; h: number; rx?: number };

const C = (cx: number, cy: number, r: number): Shape => ({ t: 'circle', cx, cy, r });
const P = (d: string, fill = false): Shape => ({ t: 'path', d, fill });

export const ICONS: Record<string, Shape[]> = {
    ok: [C(8, 8, 6), P('M5.2 8.2l2 2 3.6-4')],
    work: [C(8, 8, 6), P('M6.5 5.5v5l4-2.5z', true)],
    wait: [C(8, 5, 2.4), P('M3 14c0-3 2.3-4.6 5-4.6s5 1.6 5 4.6')],
    stuck: [P('M8 1.8l6.5 11.4h-13z'), P('M8 6.5v3M8 11.5v.1')],
    queue: [C(8, 8, 6), P('M8 4.5V8l2.3 1.5')],
    unk: [P('M8 1.5l6.5 6.5L8 14.5 1.5 8z'), P('M6.6 6.4c.2-1.5 3-1.5 2.8.2 0 1-1.4 1.1-1.4 2M8 10.8v.1')],
    no: [{ t: 'circle', cx: 8, cy: 8, r: 6, dash: '2.4 2' }, P('M5.5 8h5')],
    bot: [{ t: 'rect', x: 3, y: 5.5, w: 10, h: 7, rx: 1 }, P('M8 5.5V3M6 8.5v1.5M10 8.5v1.5')],
    done: [P('M2.5 8.5l3.5 3.5 7.5-8')],
    back: [P('M6 4L2.5 7.5 6 11M2.5 7.5H10a3.5 3.5 0 0 1 0 7H8')],
    // rail
    floor: [P('M1 13h14M2 13V7l4 2.5V7l4 2.5V4h4v9')],
    bell: [P('M8 2a4 4 0 0 1 4 4v3l1.5 2.5h-11L4 9V6a4 4 0 0 1 4-4zM6.5 13.5a1.5 1.5 0 0 0 3 0')],
    goals: [C(8, 8, 6), C(8, 8, 2.2)],
    live: [P('M1 8h3l2-5 3 10 2-5h4')],
    team: [C(5.5, 5, 2.3), C(11.3, 6, 1.8), P('M1.5 13c0-2.6 2-4 4-4s4 1.4 4 4M10.5 9.6c2 0 3.8 1 3.8 3.4')],
    runs: [P('M5.5 4L1.5 8l4 4M10.5 4l4 4-4 4')],
    autonomy: [P('M2.5 12a6 6 0 1 1 11 0'), P('M8 9l3-4')],
    plant: [P('M2 3h12M2 8h12M2 13h12'), C(5, 3, 1.3), C(10, 8, 1.3), C(7, 13, 1.3)],
    record: [P('M3 2h8l2 2v10H3zM5.5 6.5h5M5.5 9h5M5.5 11.5h3')],
    sun: [C(8, 8, 3), P('M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6L13 13M3 13l1.4-1.4M11.6 4.4L13 3')],
};
