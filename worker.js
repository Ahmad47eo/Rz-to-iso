let inputFile=null;
let outputAccess=null;
let inputReader=null;
let inputBuffer=null;
const OUT_NAME='rvz-output.iso';

async function initFiles(file,outputName){
  if(!file) throw new Error('No RVZ file was supplied.');
  inputFile=file;
  inputReader=new FileReaderSync();

  const root=await navigator.storage.getDirectory();
  const oh=await root.getFileHandle(outputName,{create:true});
  outputAccess=await oh.createSyncAccessHandle();
  outputAccess.truncate(0);

  globalThis.rvzInputSize=()=>inputFile.size;
  globalThis.rvzInputRead=(offset,length)=>{
    if(length<=0)return new Uint8Array(0);
    const start=Number(offset);
    const end=Math.min(inputFile.size,start+length);
    if(start<0||start>=inputFile.size||end<=start)return new Uint8Array(0);

    const bytes=new Uint8Array(inputReader.readAsArrayBuffer(inputFile.slice(start,end)));
    if(!inputBuffer||inputBuffer.byteLength<bytes.byteLength)inputBuffer=new Uint8Array(bytes.byteLength);
    inputBuffer.set(bytes);
    return bytes.byteLength===length?inputBuffer:inputBuffer.slice(0,bytes.byteLength);
  };
  globalThis.rvzOutputWrite=(data,offset)=>outputAccess.write(data,{at:Number(offset)});
  globalThis.rvzProgress=(done,total)=>postMessage({type:'progress',done,total});
}

let wasmReady=false;
(async()=>{
  try{
    importScripts('./wasm_exec.js');
    const go=new Go();
    const result=await WebAssembly.instantiateStreaming(fetch('./rvz.wasm'),go.importObject);
    go.run(result.instance);
    wasmReady=true;
    postMessage({type:'ready'});
  }catch(e){
    postMessage({type:'error',message:'Decoder failed to load: '+(e.message||String(e))});
  }
})();

onmessage=async e=>{
  const m=e.data||{};
  if(m.type!=='start'||!wasmReady)return;
  try{
    await initFiles(m.file,OUT_NAME);
    postMessage({type:'reading'});
    const result=globalThis.rvzConvert();
    if(result.error)throw new Error(result.error);
    outputAccess.flush();
    outputAccess.close();
    outputAccess=null;
    inputFile=null;
    inputReader=null;
    inputBuffer=null;
    postMessage({type:'done',size:result.size,name:m.name});
  }catch(e){
    try{if(outputAccess)outputAccess.close()}catch(_){}
    outputAccess=null;
    inputFile=null;
    inputReader=null;
    inputBuffer=null;
    postMessage({type:'error',message:e.message||String(e)});
  }
};