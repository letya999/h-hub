-- Resume work for open requests created before job reconciliation was installed.
INSERT INTO fsm.outbox (kind, machine_id, topic, payload)
SELECT m.kind, m.id, 'check-request-job', convert_to(m.id, 'UTF8')
FROM fsm.machines m JOIN owner_requests q ON q.id = m.id
WHERE m.kind = 'owner_request' AND m.state = 'pending' AND q.run <> '';
