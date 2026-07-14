ObjC.import("Foundation");

function stdinText() {
    const data = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
    const text = $.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding);
    return ObjC.unwrap(text);
}

function value(getter, fallback) {
    try {
        const v = getter();
        return v === null || v === undefined ? fallback : v;
    } catch (_) {
        return fallback;
    }
}

function localDate(date) {
    if (!date) return "";
    const y = date.getFullYear();
    const m = String(date.getMonth() + 1).padStart(2, "0");
    const d = String(date.getDate()).padStart(2, "0");
    return `${y}-${m}-${d}`;
}

function utcDateTime(date) {
    if (!date) return "";
    return date.toISOString().replace(/\.\d{3}Z$/, "Z");
}

function dateFromYMD(value) {
    if (!value) return null;
    const parts = value.split("-").map(Number);
    return new Date(parts[0], parts[1] - 1, parts[2], 12, 0, 0);
}

function isDateOnly(value) {
    return /^\d{4}-\d{2}-\d{2}$/.test(value);
}

function exactAccount(app, name) {
    if (!name) return null;
    const matches = app.accounts().filter(a => value(() => a.name(), "") === name);
    if (matches.length !== 1) {
        throw new Error(matches.length === 0
            ? `Reminders account not found: ${name}`
            : `More than one Reminders account is named: ${name}`);
    }
    return matches[0];
}

function resolveList(app, request) {
    const account = exactAccount(app, request.account || "");
    const lists = account ? account.lists() : app.lists();
    const matches = lists.filter(l => value(() => l.name(), "") === request.list);
    if (matches.length > 1) {
        throw new Error(`More than one Reminders list is named: ${request.list}; set reminders.account`);
    }
    if (matches.length === 1) return matches[0];
    if (!request.create_list) {
        throw new Error(`Reminders list not found: ${request.list}; rerun with --create-list`);
    }
    const target = account || app.defaultAccount();
    const list = app.List({name: request.list});
    target.lists.push(list);
    return list;
}

function reminderJSON(reminder) {
    const modified = value(() => reminder.modificationDate(), null);
    const timed = value(() => reminder.dueDate(), null);
    const allDay = value(() => reminder.alldayDueDate(), null);
    return {
        id: String(value(() => reminder.id(), "")),
        title: String(value(() => reminder.name(), "")),
        body: String(value(() => reminder.body(), "")),
        due: timed ? utcDateTime(timed) : localDate(allDay),
        priority: Number(value(() => reminder.priority(), 0)),
        completed: Boolean(value(() => reminder.completed(), false)),
        modified: modified ? modified.toISOString() : ""
    };
}

function setReminder(reminder, operation) {
    const due = operation.due || "";
    if (!due) {
        reminder.dueDate = null;
    } else if (isDateOnly(due)) {
        reminder.dueDate = null;
        reminder.alldayDueDate = dateFromYMD(due);
    } else {
        const timed = new Date(due);
        if (Number.isNaN(timed.getTime())) {
            throw new Error(`Invalid timed due value: ${due}`);
        }
        reminder.dueDate = timed;
    }
    reminder.name = operation.title;
    reminder.body = operation.body || "";
    reminder.priority = Number(operation.priority || 0);
    reminder.completed = Boolean(operation.completed);
}

function applyOperations(app, list, operations) {
    const existing = list.reminders();
    const byID = {};
    for (const reminder of existing) {
        byID[String(value(() => reminder.id(), ""))] = reminder;
    }
    const changed = [];
    for (const operation of operations || []) {
        if (operation.kind === "create") {
            const reminder = app.Reminder({name: operation.title});
            list.reminders.push(reminder);
            setReminder(reminder, operation);
            changed.push(reminderJSON(reminder));
            continue;
        }
        if (operation.kind !== "update" && operation.kind !== "delete") {
            throw new Error(`Unsupported Reminders operation: ${operation.kind}`);
        }
        const reminder = byID[operation.id];
        if (!reminder) {
            throw new Error(`Reminder not found in configured list: ${operation.id}`);
        }
        if (operation.kind === "delete") {
            const body = String(value(() => reminder.body(), "")).trimEnd();
            const marker = `[kbrd:${operation.sync_id}]`;
            if (!body.endsWith(marker)) {
                throw new Error(`Refusing to delete reminder without matching marker: ${operation.id}`);
            }
            app.delete(reminder);
            continue;
        }
        setReminder(reminder, operation);
        changed.push(reminderJSON(reminder));
    }
    return changed;
}

function run() {
    const request = JSON.parse(stdinText());
    if (request.op !== "fetch" && request.op !== "apply") {
        throw new Error(`Unsupported operation: ${request.op}`);
    }
    const app = Application("Reminders");
    const list = resolveList(app, request);
    if (request.op === "fetch") {
        return JSON.stringify({reminders: list.reminders().map(reminderJSON)});
    }
    const changed = applyOperations(app, list, request.operations);
    return JSON.stringify({reminders: changed});
}
