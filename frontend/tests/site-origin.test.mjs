import assert from 'node:assert/strict';
import test from 'node:test';
import {siteOrigin} from '../lib/site-origin.ts';
test('canonical origins reject forwarded-style paths, credentials and insecure production URLs',()=>{
 const previous={origin:process.env.SITE_ORIGIN,mode:process.env.NODE_ENV,app:process.env.APP_ENV};
 try {
  delete process.env.APP_ENV;
  process.env.NODE_ENV='production';
  delete process.env.SITE_ORIGIN;
  assert.throws(siteOrigin,/required/);
  for(const value of ['http://blog.example','http://127.0.0.1:3000','https://user:secret@blog.example','https://blog.example/path','https://blog.example?q=x','https://blog.example#x','file:///tmp']) {process.env.SITE_ORIGIN=value;assert.throws(siteOrigin);}
  process.env.SITE_ORIGIN='https://blog.example/';assert.equal(siteOrigin(),'https://blog.example');
  process.env.APP_ENV='development';process.env.SITE_ORIGIN='http://127.0.0.1:3318';assert.equal(siteOrigin(),'http://127.0.0.1:3318');
  process.env.APP_ENV='invalid';assert.throws(siteOrigin);delete process.env.APP_ENV;
  process.env.NODE_ENV='development';delete process.env.SITE_ORIGIN;assert.equal(siteOrigin(),'http://127.0.0.1:3000');
  process.env.SITE_ORIGIN='http://[::1]:3000';assert.equal(siteOrigin(),'http://[::1]:3000');
 }finally{
  if(previous.origin===undefined)delete process.env.SITE_ORIGIN;else process.env.SITE_ORIGIN=previous.origin;
  if(previous.mode===undefined)delete process.env.NODE_ENV;else process.env.NODE_ENV=previous.mode;
  if(previous.app===undefined)delete process.env.APP_ENV;else process.env.APP_ENV=previous.app;
 }
});
