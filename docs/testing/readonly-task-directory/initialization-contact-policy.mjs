// Observer classification only: it grants no server permission, issues no POST,
// and never changes a contact, page route or task destination.
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const isID = value => typeof value === 'string' && uuid.test(value) && value !== '00000000-0000-0000-0000-000000000000'

export function initializationContactRequest(url, request, botIds, apiOrigins) {
  if (request.method !== 'POST') return null
  const match = /^\/api\/bots\/([^/]+)\/sessions\/contact$/.exec(url.pathname)
  if (!match || !isID(match[1])) return null
  const botId = match[1].toLowerCase()
  const bodyCodeUnits = typeof request.postData === 'string' ? request.postData.length : null
  const bodyReported = (request.hasPostData === true && bodyCodeUnits === null) || (bodyCodeUnits !== null && bodyCodeUnits > 0)
  return { kind: 'contact-resolution', botId, inManifest: botIds.some(id => id.toLowerCase() === botId),
    inOriginManifest: apiOrigins.includes(url.origin),
    bodyReported, bodyCodeUnits, bodyEvidence: bodyReported ? 'present-or-opaque' : bodyCodeUnits === 0 ? 'reported-empty' : 'none-reported',
    noQuery: !url.search,
    allowedObservation: apiOrigins.includes(url.origin) && botIds.some(id => id.toLowerCase() === botId) && !url.search && !bodyReported,
    bindingEffect: 'unknown' }
}

export function contactResolutionMetadata(body) {
  let data
  try { data = JSON.parse(body) } catch { return { parsed: false } }
  const fields = ['id', 'bot_id', 'created_by_user_id', 'route_id']
  const validIdentity = fields.every(key => isID(data?.[key])) && !data?.parent_session_id
  return { parsed: true, validIdentity,
    ...Object.fromEntries(fields.map(key => [key, isID(data?.[key]) ? data[key].toLowerCase() : null])),
    hasParent: !!data?.parent_session_id,
    type: ['chat', 'acp_agent'].includes(data?.type) ? data.type : 'other',
    // Creation time is not a before/after proof; no metadata/title/hook text.
    createdAt: typeof data?.created_at === 'string' && Number.isFinite(Date.parse(data.created_at)) ? data.created_at : null }
}

export function contactIdentityObservation(metadata, requestBotId, knownContacts, observations) {
  const baseline = knownContacts.find(row => row.botId.toLowerCase() === requestBotId)
  const previous = observations.get(requestBotId)
  const same = row => row.sessionId.toLowerCase() === metadata.id && row.ownerIdentityId.toLowerCase() === metadata.created_by_user_id
    && (!row.routeId || row.routeId.toLowerCase() === metadata.route_id)
  const ownerChanged = [...observations.values()].some(row => row.ownerIdentityId !== metadata.created_by_user_id)
  const matchesSavedBaseline = baseline ? same(baseline) : null
  const matchesPreviousObservation = previous ? same(previous) : null
  const identityChanged = metadata.bot_id !== requestBotId || matchesSavedBaseline === false || matchesPreviousObservation === false || ownerChanged
  if (metadata.validIdentity && !identityChanged) observations.set(requestBotId, {
    sessionId: metadata.id, ownerIdentityId: metadata.created_by_user_id, routeId: metadata.route_id,
  })
  return { matchesSavedBaseline, matchesPreviousObservation, ownerChanged, identityChanged,
    bindingEffect: 'unknown', interpretation: 'Returned identity only. Reuse/adoption/creation cannot be separated without a trustworthy before-state; no binding was changed by this observer.' }
}

export function validContactConfiguration(botIds, knownContacts) {
  return Array.isArray(botIds) && botIds.every(isID) && Array.isArray(knownContacts)
    && knownContacts.every(row => isID(row?.botId) && isID(row?.sessionId) && isID(row?.ownerIdentityId) && (!row.routeId || isID(row.routeId)))
}
