// One runnable check for the first-run screen's rules: what passes as a UUID,
// and that each way the backends refuse one reads as its own advice.
import assert from 'node:assert/strict';
import { explainActivationError, looksLikeUUID } from './activation.js';

assert.ok(looksLikeUUID(' 7998568D-7ae4-4f65-a4cb-b20e78af6a87 '));
assert.ok(!looksLikeUUID('7998568d-7ae4-4f65-a4cb'));
assert.ok(!looksLikeUUID('vless://7998568d-7ae4-4f65-a4cb-b20e78af6a87@host'));

const advice = (msg) => explainActivationError(msg).split('.')[0];
assert.equal(advice('Invalid user UUID format: invalid VLESS URL'), 'Это не UUID');
assert.equal(advice('Неверный формат UUID пользователя: invalid VLESS URL'), 'Это не UUID');
assert.equal(advice('This user UUID is not registered on the server: unknown user UUID'), 'Такого пользователя не существует');
assert.equal(advice('Этот UUID пользователя не зарегистрирован на сервере'), 'Такого пользователя не существует');
assert.equal(advice('Такого пользователя не существует'), 'Такого пользователя не существует');
assert.equal(advice('Could not reach the server to check the user UUID: server unreachable'), 'Не удалось связаться с сервером');
assert.equal(advice('Не удалось связаться с сервером для проверки UUID'), 'Не удалось связаться с сервером');
assert.equal(advice('The Jorbique VPN Extended service is not running'), 'Служба Jorbique VPN Extended не запущена');
assert.equal(advice('Permission error: set-singbox-config needs the Jorbique VPN Extended daemon, which is not running.'), 'Служба Jorbique VPN Extended не запущена');
assert.equal(explainActivationError('Не удалось сохранить настройки'), 'Не удалось сохранить настройки');
assert.match(explainActivationError('Достигнут лимит устройств.'), /на странице подписки/);
console.log('activation ok');
