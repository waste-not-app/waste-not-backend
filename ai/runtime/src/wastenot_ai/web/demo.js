'use strict';
const $ = id => document.getElementById(id);
const nonce = document.querySelector('meta[name="wastenot-demo"]').content;
let config, stream = null, photoBlob = null, photoURL = null, detection = null, selected = null;
let manualMode = false, cameraVersion = 0, flowVersion = 0, recVersion = 0;
let detectController = null, recController = null, detectRequestId = null, recRequestId = null;
let photoExpiry = null, catalogFilter = 'all', detecting = false, recommending = false;
const messages = {
  AI_BUSY: 'Model sedang menyelesaikan proses sebelumnya. Tunggu sebentar lalu coba lagi.',
  AI_TIMEOUT: 'Proses melewati batas waktu. Foto dan isian tetap tersedia; kamu dapat mencoba lagi.',
  AI_UNAVAILABLE: 'Model belum tersedia. Periksa terminal demo dan coba lagi.',
  FILE_TOO_LARGE: 'Ukuran gambar maksimal 5 MiB. Pilih foto yang lebih kecil.',
  UNSUPPORTED_MEDIA_TYPE: 'Gunakan gambar JPEG atau PNG.', INVALID_IMAGE: 'Gambar tidak dapat dibaca.',
  IMAGE_DIMENSIONS_EXCEEDED: 'Ukuran gambar terlalu besar. Pilih sisi maksimal 4096 piksel dan 12 megapiksel.',
  DEMO_SESSION_REQUIRED: 'Sesi demo berakhir. Muat ulang halaman untuk memulai lagi.',
  DETECTION_NOT_FOUND: 'Hasil deteksi sudah tidak tersedia. Ambil atau kirim foto lagi.',
  AI_SESSION_EXPIRED: 'Sesi foto berakhir. Mulai scan baru.',
  DEMO_HOURLY_LIMIT: 'Batas 30 permintaan rekomendasi per jam pada demo ini telah tercapai.',
  DEMO_SESSION_LIMIT: 'Sesi demo penuh. Bersihkan hasil lama atau mulai ulang server.',
  INVALID_CONTEXT: 'Periksa kembali jenis benda, material dan kondisi yang dipilih.',
  OBJECT_CONFIRMATION_REQUIRED: 'Konfirmasikan objek dan kondisi terlebih dahulu.',
  OBJECT_REQUIRES_MANUAL: 'Objek ini memerlukan pilihan manual dari katalog.',
  IDEMPOTENCY_CONFLICT: 'Data permintaan berubah. Bersihkan dan kirim ulang foto.',
  LLM_NOT_CONFIGURED: 'Gemini belum dikonfigurasi. Isi GEMINI_API_KEY di ai/.env lalu mulai ulang demo, atau pilih simulasi.',
  LLM_MODEL_NOT_CONFIGURED: 'Pilih GEMINI_MODEL yang dapat diakses akunmu, lalu mulai ulang demo.',
  LLM_UNAVAILABLE: 'Gemini belum dapat diakses. Periksa koneksi, model dan akses API pada akunmu.',
  LLM_RATE_LIMITED: 'Kuota Gemini sedang dibatasi. Tunggu atau gunakan mode simulasi.',
  LLM_TIMEOUT: 'Gemini belum merespons dalam batas waktu. Kamu tetap bisa melihat panduan atau memilih manual.',
  LLM_BLOCKED_OR_INCOMPLETE: 'Respons Gemini diblokir atau belum lengkap; saran tidak digunakan.',
  NO_REVIEWED_GUIDE: 'Belum ada panduan yang ditinjau untuk konteks ini. Untuk eksperimen draf, jalankan demo dengan --demo-guides.',
  NO_ALLOWED_METHOD: 'Kondisi ini belum memiliki metode otomatis dalam panduan. Lengkapi kondisi yang memang diketahui atau pilih penanganan manual.',
  INVALID_MODEL_OUTPUT: 'Saran model gagal validasi. Metode otomatis tidak digunakan.',
  INVALID_EVIDENCE: 'Saran tidak didukung evidence panduan yang cocok. Pilih penanganan manual.',
  RISK_DETAILS_REQUIRED: 'Jelaskan jenis risiko yang dicentang. Sisa minuman biasa berbeda dari bahan berbahaya.',
  CONTEXT_REQUIRED: 'Lengkapi kondisi, jenis kotoran atau kemampuan dibersihkan yang belum diketahui. Reduce tidak digunakan sebagai pengganti jawaban.',
  RISK_NOT_COVERED: 'Panduan saat ini belum mencakup kontaminasi ini. Minta arahan pengelola limbah yang sesuai.'
};
const rawNames = {'bottle':'Botol','cup':'Gelas / cangkir','bowl':'Mangkuk','wine glass':'Gelas berkaki',
  'aluminium_can':'Kaleng aluminium','glass_bottle':'Botol kaca','hdpe_bottle':'Botol HDPE · konfirmasi isi','plastic_bottle':'Botol plastik · konfirmasi PET',
  'fork':'Garpu','spoon':'Sendok','knife':'Pisau','vase':'Vas','person':'Orang','dining table':'Meja makan',
  'cell phone':'Ponsel','book':'Buku','pizza':'Piza','chair':'Kursi','laptop':'Laptop','tv':'Televisi / monitor'};
function rawLabel(raw) {
  const broad = {'can':'Kaleng · konfirmasi bahan','plastic_bag':'Kantong plastik',
    'paper':'Kertas','cardboard':'Kardus','drink_carton':'Karton minuman',
    'plastic_wrapper':'Pembungkus / film plastik','food_container':'Wadah makanan · konfirmasi bahan',
    'bottle_cap':'Tutup botol · konfirmasi bahan'};
  if (broad[raw]) return broad[raw];
  return rawNames[raw] || config?.catalog.items.find(x => x.baseline_raw_class === raw)?.label || raw;
}
function message(text, kind = '') {
  $('global-message').textContent = text;
  $('global-message').className = `message ${kind}`;
  $('global-message').hidden = !text;
}
function setStep(number) {
  for (let i = 1; i <= 4; i++) {
    if (i === number) $(`step-${i}`).setAttribute('aria-current', 'step');
    else $(`step-${i}`).removeAttribute('aria-current');
  }
}
async function api(path, options = {}) {
  let response;
  try {
    response = await fetch(`/demo/api/${path}`, {...options,
      headers: {'X-WasteNot-Demo': nonce, ...options.headers}});
  } catch (error) {
    if (error.name === 'AbortError') throw error;
    throw new Error('Tidak dapat terhubung ke server demo. Periksa terminal dan mulai ulang bila perlu.');
  }
  if (response.status === 204) return null;
  const data = await response.json();
  if (!response.ok) throw new Error(messages[data.detail] || 'Permintaan belum berhasil. Periksa isian atau mulai scan baru.');
  return data;
}
function stopCamera() {
  cameraVersion++;
  if (stream) stream.getTracks().forEach(track => track.stop());
  stream = null;
  $('camera').srcObject = null;
  $('camera').hidden = true;
  $('live-label').hidden = true;
  $('capture').hidden = true;
  $('capture').disabled = true;
  $('start-camera').hidden = false;
  $('camera-status').textContent = photoBlob ? 'Foto siap ditinjau' : 'Kamera berhenti';
}
async function startCamera() {
  stopCamera();
  const version = cameraVersion;
  if (!navigator.mediaDevices?.getUserMedia) {
    message('Kamera browser tidak tersedia. Buka lewat http://127.0.0.1 atau gunakan tombol Pilih foto.', 'warning');
    return;
  }
  $('camera-status').textContent = 'Menunggu izin kamera…';
  try {
    const next = await navigator.mediaDevices.getUserMedia({audio: false,
      video: {facingMode: {ideal: 'environment'}, width: {ideal: 1280}, height: {ideal: 720}}});
    if (version !== cameraVersion) { next.getTracks().forEach(track => track.stop()); return; }
    stream = next;
    $('camera').srcObject = stream;
    $('camera').hidden = false;
    await $('camera').play();
    if (version !== cameraVersion) return;
    $('photo-stage').hidden = true;
    $('camera-placeholder').hidden = true;
    $('live-label').hidden = false;
    $('start-camera').hidden = true;
    $('capture').hidden = false;
    $('capture').disabled = false;
    $('camera-status').textContent = 'Kamera aktif';
    message('Foto baru dikirim ke YOLO setelah kamu mengambil foto dan menekan Kirim untuk deteksi.');
  } catch (error) {
    if (version !== cameraVersion) return;
    stopCamera();
    $('camera-placeholder').hidden = !!photoBlob;
    $('photo-stage').hidden = !photoBlob;
    const text = error.name === 'NotAllowedError' ? 'Izin kamera belum diberikan. Izinkan kamera di browser, atau pilih foto dari perangkat.' :
      error.name === 'NotFoundError' ? 'Tidak ada kamera yang ditemukan. Gunakan tombol Pilih foto.' :
      'Kamera belum dapat dibuka. Tutup aplikasi lain yang memakai kamera, lalu coba lagi atau pilih foto.';
    message(text, 'warning');
  }
}
function clearRecommendation() {
  recVersion++;
  recController?.abort();
  recController = null;
  recommending = false;
  recRequestId = null;
  $('recommendation-result').hidden = true;
  $('recommendation-empty').hidden = false;
  $('recommendation-empty').querySelector('p').textContent = 'Setelah objek dan kondisi dikonfirmasi, saran akan muncul di sini.';
  $('recommend').innerHTML = 'Minta rekomendasi <span aria-hidden="true">↗</span>';
  updateForm();
}
function resetConditions() {
  ['cleanliness','integrity','reusable','cleanable','soiling','risk-type','former-contents'].forEach(id => { $(id).value = 'UNKNOWN'; });
  $('reuse-intention').value = '';
  $('recommendation-goal').value = 'AUTO';
  $('recycling-access').value = 'UNKNOWN';
  $('confirmed').checked = false;
  $('risk').checked = false;
  updateConditionDetails();
  $('former-contents-field').hidden = true;
}
function updateConditionDetails() {
  const dirty = $('cleanliness').value === 'DIRTY';
  $('cleaning-details').hidden = !dirty;
  if (!dirty) { $('cleanable').value = 'UNKNOWN'; $('soiling').value = 'UNKNOWN'; }
  $('risk-details').hidden = !$('risk').checked;
  if (!$('risk').checked) $('risk-type').value = 'UNKNOWN';
}
function updateForm() {
  $('recommend').disabled = !config || !$('object-key').value || !$('confirmed').checked || recommending || detecting;
}
function clearAnalysis() {
  flowVersion++;
  detectController?.abort();
  detectController = null;
  detection = null; selected = null; manualMode = false; detecting = false; detectRequestId = null;
  $('boxes').replaceChildren(); $('object-list').replaceChildren(); $('detection-section').hidden = true;
  $('detect').textContent = 'Kirim untuk deteksi'; $('detect').disabled = false;
  $('selected-label').textContent = 'Belum dipilih';
  resetConditions(); fillObjectOptions([]); clearRecommendation(); setStep(1);
}
async function resetServer() {
  if (config) await api('session', {method: 'DELETE'});
}
async function clearFlow() {
  stopCamera(); clearAnalysis();
  clearTimeout(photoExpiry);
  if (photoURL) URL.revokeObjectURL(photoURL);
  photoURL = null; photoBlob = null;
  $('photo').removeAttribute('src'); $('photo-stage').hidden = true; $('camera-placeholder').hidden = false;
  $('detect').hidden = true; $('retake').hidden = true; $('file').value = '';
  $('camera-status').textContent = 'Kamera berhenti';
  message('Foto dan hasil sementara dibersihkan. Kamu bisa mulai lagi.');
  try { await resetServer(); } catch (error) { message(error.message, 'warning'); }
}
async function preparePhoto(blob) {
  stopCamera(); clearAnalysis();
  const version = flowVersion;
  try { await resetServer(); } catch (error) { message(error.message, 'error'); return; }
  if (version !== flowVersion) return;
  if (photoURL) URL.revokeObjectURL(photoURL);
  photoBlob = blob; photoURL = URL.createObjectURL(blob);
  $('photo').src = photoURL; $('photo-stage').hidden = false; $('camera-placeholder').hidden = true;
  $('detect').hidden = false; $('retake').hidden = false; $('start-camera').hidden = true;
  $('camera-status').textContent = 'Tinjau foto';
  message('Periksa fotonya. Tekan Kirim untuk deteksi ketika sudah siap.');
  clearTimeout(photoExpiry);
  photoExpiry = setTimeout(() => { clearFlow(); message('Foto sementara dibersihkan setelah 15 menit. Mulai scan baru bila perlu.'); }, 15 * 60 * 1000);
}
function canvasBlob(canvas) { return new Promise((resolve,reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('Gagal mengambil foto.')), 'image/jpeg', .9)); }
async function capture() {
  if (!$('camera').videoWidth) { message('Tunggu sampai pratinjau kamera siap.', 'warning'); return; }
  const version = ++flowVersion;
  const canvas = document.createElement('canvas');
  const ratio = Math.min(1,1600 / Math.max($('camera').videoWidth,$('camera').videoHeight));
  canvas.width = Math.round($('camera').videoWidth * ratio); canvas.height = Math.round($('camera').videoHeight * ratio);
  canvas.getContext('2d').drawImage($('camera'),0,0,canvas.width,canvas.height);
  stopCamera();
  try { const blob = await canvasBlob(canvas); if (version === flowVersion) await preparePhoto(blob); } catch (error) { message(error.message,'error'); }
  canvas.width = 0; canvas.height = 0;
}
async function filePhoto(file) {
  if (!file) return;
  if (!['image/jpeg','image/png'].includes(file.type) || file.size > 5 * 1024 * 1024) {
    message('Pilih gambar JPEG/PNG maksimal 5 MiB.','error'); return;
  }
  const version = ++flowVersion;
  let bitmap;
  try {
    bitmap = await createImageBitmap(file);
    if (bitmap.width * bitmap.height > 12000000 || Math.max(bitmap.width,bitmap.height) > 4096) throw new Error(messages.IMAGE_DIMENSIONS_EXCEEDED);
    const scale = Math.min(1,1600 / Math.max(bitmap.width,bitmap.height));
    const canvas = document.createElement('canvas'); canvas.width = Math.round(bitmap.width*scale); canvas.height = Math.round(bitmap.height*scale);
    canvas.getContext('2d').drawImage(bitmap,0,0,canvas.width,canvas.height);
    const blob = await canvasBlob(canvas);
    if (version === flowVersion) await preparePhoto(blob);
    canvas.width = 0; canvas.height = 0;
  } catch (error) { message(error.message || 'Foto belum dapat dibaca.','error'); }
  finally { bitmap?.close(); }
}
async function detectPhoto() {
  if (!photoBlob || detecting) return;
  detectRequestId ||= crypto.randomUUID();
  const version = flowVersion;
  detecting = true; updateForm(); clearRecommendation();
  $('detect').disabled = true; $('detect').textContent = 'YOLO sedang memeriksa…';
  detectController = new AbortController();
  message('Menjalankan deteksi pada foto. Kamera tidak mengirim video secara terus-menerus.');
  try {
    const result = await api('detect',{method:'POST',body:photoBlob,signal:detectController.signal,
      headers:{'Content-Type':'image/jpeg','X-Client-Request-Id':detectRequestId}});
    if (version !== flowVersion) return;
    detection = result.data; selected = null; manualMode = false;
    renderDetections(); setStep(2);
    const count = detection.objects.filter(x => x.supported_for_review).length;
    message(count ? 'Pilih satu objek pada bingkai foto atau daftar di bawahnya, lalu konfirmasi jenis dan kondisinya.' :
      'Belum ada objek yang bisa ditawarkan. Coba foto ulang atau pilih benda dari katalog secara manual.',count ? '' : 'warning');
  } catch (error) { if (version === flowVersion && error.name !== 'AbortError') message(error.message,'error'); }
  finally {
    if (version === flowVersion) { detecting = false; $('detect').disabled = false; $('detect').textContent = 'Deteksi ulang'; updateForm(); }
  }
}
function renderDetections() {
  $('boxes').replaceChildren(); $('object-list').replaceChildren(); $('detection-section').hidden = false;
  $('detection-summary').textContent = `${detection.objects.length} kandidat`;
  for (const object of detection.objects) {
    const label = rawLabel(object.raw_class), score = Math.round(object.confidence*100);
    const button = document.createElement('button');
    button.type = 'button'; button.className = `bbox ${object.supported_for_review ? '' : 'unsupported'} ${selected?.object_id === object.object_id ? 'selected' : ''}`;
    button.disabled = !object.supported_for_review;
    const [x1,y1,x2,y2] = object.bbox;
    Object.assign(button.style,{left:`${x1*100}%`,top:`${y1*100}%`,width:`${(x2-x1)*100}%`,height:`${(y2-y1)*100}%`,
      zIndex:String(object.supported_for_review ? Math.round((1-(x2-x1)*(y2-y1))*1000)+1 : 0),
      pointerEvents:object.supported_for_review ? 'auto' : 'none'});
    const span = document.createElement('span'); span.textContent = `${label} · ${score}%`; button.append(span);
    button.setAttribute('aria-label',`Pilih ${label}, skor deteksi ${score}%`);
    button.addEventListener('click',() => selectObject(object)); $('boxes').append(button);
    const item = document.createElement('button'); item.type = 'button'; item.className = `object-choice ${selected?.object_id === object.object_id ? 'selected' : ''}`;
    item.disabled = !object.supported_for_review; item.setAttribute('aria-pressed',String(selected?.object_id === object.object_id));
    const title = document.createElement('strong'); title.textContent = label;
    const detail = document.createElement('span'); detail.textContent = `${score}% · ${object.ambiguous_with?.length ? 'Kelas bertumpuk · periksa' : object.confidence_band === 'UNCERTAIN' ? 'Perlu ditinjau' : object.supported_for_review ? 'Pilih objek' : 'Pilih manual'}`;
    item.append(title,detail); item.addEventListener('click',() => selectObject(object)); $('object-list').append(item);
  }
}
function fillObjectOptions(suggested) {
  const select = $('object-key'); select.replaceChildren(new Option('Konfirmasikan jenis / material',''));
  if (!config) return;
  if (suggested.length) {
    const group = document.createElement('optgroup'); group.label = 'Sesuai objek yang terlihat';
    config.catalog.items.filter(x => suggested.includes(x.object_key)).forEach(x => group.append(new Option(x.label,x.object_key)));
    select.append(group);
  }
  const others = document.createElement('optgroup'); others.label = suggested.length ? 'Koreksi ke objek lain' : 'Katalog benda';
  config.catalog.items.filter(x => !suggested.includes(x.object_key)).forEach(x => others.append(new Option(x.label,x.object_key)));
  select.append(others);
}
function selectObject(object) {
  selected = object; manualMode = false; resetConditions(); clearRecommendation();
  fillObjectOptions(object.suggested_object_keys || []);
  $('selected-label').textContent = rawLabel(object.raw_class);
  $('context-help').textContent = object.ambiguous_with?.length ? 'Ada beberapa tebakan kelas pada area yang sama. Bandingkan lewat daftar objek, lalu konfirmasikan material yang benar.' : object.confidence_band === 'UNCERTAIN' ? 'Prediksi ini perlu ditinjau. Skor tinggi belum membuktikan jenis/material benar.' : 'Objek dipilih. Konfirmasikan jenis/material dan kondisi yang kamu ketahui.';
  renderDetections(); setStep(3); updateForm();
}
function chooseManual(key = '') {
  stopCamera();
  flowVersion++;
  detectController?.abort(); detecting = false;
  $('detect').disabled = false; $('detect').textContent = 'Kirim untuk deteksi';
  $('photo-stage').hidden = !photoBlob; $('camera-placeholder').hidden = !!photoBlob;
  selected = null; manualMode = true; resetConditions(); clearRecommendation(); fillObjectOptions([]);
  $('object-key').value = key; $('selected-label').textContent = 'Pilihan manual';
  $('context-help').textContent = 'Objek dipilih dari katalog olehmu. Hasil ini tidak dianggap sebagai identifikasi YOLO.';
  if (detection) renderDetections(); setStep(3); updateMaterialNote(); updateForm();
  $('context-title').scrollIntoView({behavior:'smooth',block:'center'});
}
function updateMaterialNote() {
  const item = config?.catalog.items.find(x => x.object_key === $('object-key').value);
  $('former-contents-field').hidden = !(item?.object_key === 'hdpe_bottle' || item?.guide_template === 'broad_food_container');
  $('material-note').textContent = item?.special_handling ? 'Saran tetap memilih satu metode. Ikuti peringatan dan rute pengelola khusus untuk benda ini.' :
    item?.object_key === 'hdpe_bottle' ? 'Konfirmasi HDPE/kode 2 dan isi sebelumnya. Kode resin tidak menjamin aman dipakai ulang. Panduan ini hanya mencakup bekas pangan/minuman.' :
    item?.guide_template === 'broad_food_container' ? 'Konfirmasikan isi sebelumnya. Panduan ini hanya mencakup bekas pangan/minuman; kemasan bekas bahan kimia memerlukan pengelola khusus.' :
    item?.guide_template === 'food' ? 'Saran Reduce membantu mencegah sisa makanan berikutnya. Foto tidak memastikan makanan aman dikonsumsi.' :
    item?.guide_template === 'toothbrush' ? 'Konfirmasikan sikat gigi plastik manual, tanpa baterai/listrik. Sikat elektrik harus mengikuti panduan elektronik khusus.' :
    item?.guide_template === 'tableware' ? 'Utuh dan masih layak? Peralatan kotor biasa bisa disarankan Reuse setelah dicuci; isi jenis kotoran dan kemampuan dibersihkan.' :
    item?.guide_template === 'reuse' ? 'Untuk saran pakai ulang, konfirmasikan kebersihan, keutuhan, kelayakan dan niat penggunaan kembali.' :
    'Bentuk benda belum memastikan bahannya. Konfirmasikan material yang kamu ketahui.';
}
function providerNote() {
  $('provider-note').textContent = $('provider').value === 'fixture' ? 'Mode simulasi dipilih. Tidak ada request ke Gemini; hasil selalu berlabel simulasi.' :
    config?.gemini_configured ? `Rekomendasi akan diminta langsung ke Gemini (${config.gemini_model}). Foto tidak dikirim.` :
      'Gemini belum dikonfigurasi. Isi GEMINI_API_KEY dan GEMINI_MODEL di ai/.env, lalu mulai ulang demo.';
}
function renderRecommendation(result) {
  $('recommendation-empty').hidden = true; $('recommendation-result').hidden = false;
  const methodNames = {REDUCE:'Reduce',REUSE:'Reuse',RECYCLE:'Recycle',TRASH:'Trash'};
  $('method').textContent = result.status === 'READY' ? methodNames[result.recommended_method] : result.status === 'NEEDS_CONTEXT' ? 'Lengkapi kondisi' : 'Saran belum tersedia';
  $('recommendation-source').textContent = result.status === 'READY' ? (result.provider === 'fixture' ? 'Simulasi' : 'Gemini langsung') : 'Panduan & kebijakan';
  $('reason').textContent = result.reason;
  $('recommendation-basis').hidden = result.status !== 'READY';
  $('recommendation-basis').textContent = result.recommendation_basis === 'CONDITIONAL' ?
    'Saran sementara · periksa syarat sebelum bertindak' : 'Berdasarkan kondisi yang kamu isi';
  $('reason-code').hidden = !result.reason_code;
  $('reason-code').textContent = messages[result.reason_code] || result.reason_code || '';
  if (result.missing_context?.length) {
    const names = {'risk_type':'jenis risiko','condition.cleanliness':'kebersihan','condition.integrity':'keutuhan',
      'condition.reusable':'kelayakan pakai','condition.cleanable':'kemampuan dibersihkan','condition.soiling':'jenis kotoran',
      'condition.former_contents':'isi sebelumnya',
      'reuse_intention':'niat pakai ulang','recycling_access':'akses daur ulang','object_key':'jenis benda/material'};
    $('reason-code').textContent += ' Isian: '+result.missing_context.map(field => names[field] || field).join(', ')+'.';
  }
  $('recommendation-scope').hidden = !result.recommendation_scope;
  $('recommendation-scope').textContent = result.recommendation_scope === 'PREVENTION' ?
    'Pencegahan untuk penggunaan berikutnya. Ini bukan instruksi pembuangan benda saat ini.' :
    result.recommendation_basis === 'CONDITIONAL' ? 'Rencana penanganan bersyarat. Kondisi yang kosong tetap belum diketahui.' : 'Penanganan untuk benda yang dikonfirmasi.';
  $('handling-steps').replaceChildren();
  for (const step of result.steps) { const li = document.createElement('li'); li.textContent = step; $('handling-steps').append(li); }
  $('caution').hidden = !result.caution;
  $('caution').textContent = result.special_handling ? `Perlu penanganan khusus: ${result.caution}` : result.caution;
  $('source-list').replaceChildren();
  for (const url of result.source_urls) {
    if (!url.startsWith('https://')) continue;
    const li = document.createElement('li'), link = document.createElement('a');
    const reference = result.source_details?.find(source => source.url === url);
    link.href = url; link.textContent = reference?.title || new URL(url).hostname + new URL(url).pathname; link.target = '_blank'; link.rel = 'noopener noreferrer';
    li.append(link);
    if (reference) {
      const note = document.createElement('p'); note.className = 'field-help';
      note.textContent = `${reference.publisher} · ${reference.region}${reference.checked_at ? ' · diperiksa '+reference.checked_at : ''}. ${reference.scope_note}`;
      li.append(note);
    }
    $('source-list').append(li);
  }
  $('evidence-note').textContent = `${result.guide_status === 'DRAFT' ? 'Panduan draf untuk demo; belum review lokal. ' : ''}${result.evidence_ids.length ? 'Evidence: '+result.evidence_ids.join(', ') : 'Tidak ada metode otomatis yang dipilih.'}`;
  setStep(4);
}
async function requestRecommendation(event) {
  event.preventDefault();
  if (recommending || !$('object-key').value || !$('confirmed').checked || (!selected && !manualMode)) return;
  const version = recVersion;
  const intention = $('reuse-intention').value;
  const item = config.catalog.items.find(x => x.object_key === $('object-key').value);
  recRequestId ||= crypto.randomUUID(); recommending = true; updateForm();
  $('recommend').textContent = $('provider').value === 'gemini' ? 'Meminta saran Gemini…' : 'Menjalankan simulasi…';
  recController = new AbortController();
  const input = {client_request_id:recRequestId,detection_id:manualMode ? null : detection.detection_id,
    object_id:manualMode ? null : selected.object_id,manual:manualMode,confirmed_object:true,provider:$('provider').value,
    context:{object_key:item.object_key,category:item.category,condition:{cleanliness:$('cleanliness').value,
      integrity:$('integrity').value,reusable:$('reusable').value,cleanable:$('cleanable').value,soiling:$('soiling').value,former_contents:$('former-contents').value},reuse_intention:intention === '' ? null : intention === 'true',risk_flag:$('risk').checked,
      risk_type:$('risk-type').value,
      recommendation_goal:$('recommendation-goal').value,recycling_access:$('recycling-access').value}};
  try {
    const response = await api('recommend',{method:'POST',signal:recController.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});
    if (version !== recVersion) return;
    renderRecommendation(response.data);
    if (response.data.status !== 'READY') recRequestId = null;
    message(response.data.status === 'READY' ? 'Saran siap ditinjau. Periksa sumber dan pastikan sesuai dengan tindakan yang akan kamu lakukan.' :
      (messages[response.data.reason_code] || 'Saran otomatis belum tersedia. Kamu tetap bisa melihat panduan dan melanjutkan manual.'),response.data.status === 'READY' ? '' : 'warning');
  } catch (error) { if (version === recVersion && error.name !== 'AbortError') message(error.message,'error'); }
  finally { if (version === recVersion) { recommending = false; $('recommend').innerHTML = 'Minta rekomendasi <span aria-hidden="true">↗</span>'; updateForm(); } }
}
function renderCatalog() {
  const query = $('catalog-search').value.trim().toLocaleLowerCase('id'); $('catalog').replaceChildren();
  for (const item of config.catalog.items) {
    const modelLabel = config.supported_object_labels[item.object_key];
    const supported = Boolean(modelLabel);
    if (catalogFilter === 'detected' && !supported || catalogFilter === 'manual' && supported || catalogFilter === 'special' && !item.special_handling) continue;
    if (query && !`${item.label} ${item.object_key} ${item.baseline_raw_class || ''} ${item.category}`.toLocaleLowerCase('id').includes(query)) continue;
    const button = document.createElement('button'); button.type = 'button'; button.className = 'catalog-item';
    const title = document.createElement('strong'), note = document.createElement('small'); title.textContent = item.label;
    note.textContent = item.special_handling ? 'Penanganan khusus' : !supported ? 'Pilih manual · belum didukung model aktif' : `${rawLabel(modelLabel)} · konfirmasi material`;
    button.append(title,note); button.addEventListener('click',() => chooseManual(item.object_key)); $('catalog').append(button);
  }
  if (!$('catalog').children.length) { const p = document.createElement('p'); p.textContent = 'Tidak ada objek yang cocok dengan pencarian.'; $('catalog').append(p); }
}
async function initialize() {
  try {
    config = await api('config');
    const supported = config.supported_visual_classes;
    $('engine-status').textContent = 'YOLO siap · '+(config.gemini_configured ? 'Gemini dikonfigurasi' : 'Gemini perlu konfigurasi');
    $('catalog-summary').textContent = `${supported} kelas visual aktif mendukung ${Object.keys(config.supported_object_labels).length} dari ${config.catalog.items.length} pilihan benda/material. Pilihan lainnya tersedia secara manual.`;
    $('draft-label').textContent = config.demo_guides ? 'Panduan draf · demo' : 'Panduan ditinjau saja';
    fillObjectOptions([]); renderCatalog(); providerNote(); updateForm();
    // Running the demo opens the camera permission flow; upload still needs a click.
    if (new URLSearchParams(location.search).get('camera') !== 'off') await startCamera();
  } catch (error) { message(error.message,'error'); $('engine-status').textContent = 'Belum terhubung'; }
}
$('start-camera').addEventListener('click',startCamera);
$('capture').addEventListener('click',capture);
$('detect').addEventListener('click',detectPhoto);
$('retake').addEventListener('click',async () => { await clearFlow(); await startCamera(); });
$('clear').addEventListener('click',clearFlow);
$('choose-file').addEventListener('click',() => $('file').click());
$('file').addEventListener('change',() => filePhoto($('file').files[0]));
$('manual').addEventListener('click',() => chooseManual());
$('context-form').addEventListener('submit',requestRecommendation);
$('object-key').addEventListener('change',() => { resetConditions(); clearRecommendation(); updateMaterialNote(); setStep(3); });
['cleanliness','integrity','reusable','cleanable','soiling','former-contents','reuse-intention','recommendation-goal','recycling-access','risk','risk-type','confirmed','provider'].forEach(id => $(id).addEventListener('change',() => {
  if (id === 'soiling' && $('soiling').value === 'HAZARDOUS') { $('risk').checked = true; $('risk-type').value = 'CHEMICAL'; }
  if (id === 'former-contents' && $('former-contents').value === 'CHEMICAL') { $('risk').checked = true; $('risk-type').value = 'CHEMICAL'; }
  updateConditionDetails(); clearRecommendation(); providerNote(); setStep(3);
}));
$('catalog-search').addEventListener('input',renderCatalog);
$('catalog-filters').addEventListener('click',event => { const button = event.target.closest('button[data-filter]'); if (!button) return; catalogFilter = button.dataset.filter; for (const b of $('catalog-filters').children) b.classList.toggle('active',b === button); renderCatalog(); });
document.addEventListener('visibilitychange',() => { if (document.hidden) stopCamera(); });
window.addEventListener('pagehide',() => { stopCamera(); detectController?.abort(); recController?.abort(); if (photoURL) URL.revokeObjectURL(photoURL); fetch('/demo/api/session',{method:'DELETE',headers:{'X-WasteNot-Demo':nonce},keepalive:true}).catch(() => {}); });
initialize();
