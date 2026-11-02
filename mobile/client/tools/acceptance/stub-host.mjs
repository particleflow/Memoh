// Emulator-side host wrapper around the scripted service in stub-service.mjs.
//
// The Chromium harness (level 2) serves the bundle from the stub's own origin,
// so its API calls are same-origin. On the phone the packaged bundle runs on
// begonia-client://app, which makes every call cross-origin, and the client
// also keeps its stream cursor across an app restart. Two consequences the
// Chromium harness cannot show:
//
//   1. every response needs an access-control-allow-origin header;
//   2. ids <= cursor are dropped, so the stub's per-scenario seq restart makes
//      a new reply look stale and the stream goes silent. A real server opens a
//      new epoch instead of rewinding a session's sequence.
//
// Both are fixed here, outside the reviewed stub file.
import { ServerResponse } from 'node:http'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const originalWriteHead = ServerResponse.prototype.writeHead
ServerResponse.prototype.writeHead = function acceptanceWriteHead(statusCode, ...rest) {
  let headers = rest[rest.length - 1]
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) {
    headers = {}
    rest.push(headers)
  }
  if (!Object.keys(headers).some(key => key.toLowerCase() === 'access-control-allow-origin')) {
    headers['access-control-allow-origin'] = '*'
  }
  return originalWriteHead.call(this, statusCode, ...rest)
}

const { StubService } = await import('./stub-service.mjs')

const here = dirname(fileURLToPath(import.meta.url))
// ACCEPTANCE_CLIENT_WEB names the packaged bundle directory directly; the
// project-root form is a convenience for the default layout.
const bundle = process.env.ACCEPTANCE_CLIENT_WEB
  ? resolve(process.env.ACCEPTANCE_CLIENT_WEB)
  : resolve(process.env.ACCEPTANCE_EMULATOR_PROJECT || here,
    'app/entry/src/main/resources/rawfile/client-web')
const port = Number(process.env.STUB_PORT || 18081)
const deviceHost = process.env.ACCEPTANCE_DEVICE_HOST || '10.0.2.2'

const service = new StubService({ bundleDir: bundle, port })
const runtimeFor = service.runtimeFor.bind(service)
service.runtimeFor = sessionId => {
  const state = runtimeFor(sessionId)
  state.epoch = `epoch-${service.generation + 1}`
  return state
}

// The packaged bundle sets its token here, and level 2 seeds localStorage
// directly because Chromium can. On the phone the harness cannot seed storage
// before the document boots, so the scripted service answers the sign-in call
// the packaged login form actually makes. Level 2 never exercised this route,
// so the same login screen is otherwise a dead end on the device.
const api = service.api.bind(service)
service.api = async (req, res, path, url) => {
  if (path === '/auth/login' && req.method === 'POST') {
    service.apiLog.push({ method: 'POST', path, at: new Date().toISOString() })
    await service.readBody(req)
    // Shape of the SDK's HandlersLoginResponse; the login page requires
    // access_token and user_id and reports anything else as a server error.
    service.json(res, 200, {
      access_token: 'acceptance-token', token_type: 'Bearer',
      expires_at: new Date(Date.now() + 86400000).toISOString(),
      user_id: 'user-stub', username: 'acceptance', display_name: 'Acceptance', role: 'admin', timezone: 'UTC',
    })
    return
  }
  return api(req, res, path, url)
}

await new Promise((done, fail) => {
  service.server.listen(port, '0.0.0.0', done).on('error', fail)
})
service.port = port
service.apiOrigin = `http://${deviceHost}:${port}`
console.log(JSON.stringify({ listening: `http://0.0.0.0:${port}`, bundle, origin: service.apiOrigin }))
