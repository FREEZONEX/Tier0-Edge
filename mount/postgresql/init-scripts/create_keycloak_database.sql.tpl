-- 创建keycloak数据库
CREATE DATABASE keycloak;

-- 切换到 Keycloak 数据库

\c keycloak;


-- ----------------------------
-- Table structure for admin_event_entity
-- ----------------------------
DROP TABLE IF EXISTS "public"."admin_event_entity";
CREATE TABLE "public"."admin_event_entity" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "admin_event_time" int8,
  "realm_id" varchar(255) COLLATE "pg_catalog"."default",
  "operation_type" varchar(255) COLLATE "pg_catalog"."default",
  "auth_realm_id" varchar(255) COLLATE "pg_catalog"."default",
  "auth_client_id" varchar(255) COLLATE "pg_catalog"."default",
  "auth_user_id" varchar(255) COLLATE "pg_catalog"."default",
  "ip_address" varchar(255) COLLATE "pg_catalog"."default",
  "resource_path" varchar(2550) COLLATE "pg_catalog"."default",
  "representation" text COLLATE "pg_catalog"."default",
  "error" varchar(255) COLLATE "pg_catalog"."default",
  "resource_type" varchar(64) COLLATE "pg_catalog"."default",
  "details_json" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of admin_event_entity
-- ----------------------------

-- ----------------------------
-- Table structure for associated_policy
-- ----------------------------
DROP TABLE IF EXISTS "public"."associated_policy";
CREATE TABLE "public"."associated_policy" (
  "policy_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "associated_policy_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of associated_policy
-- ----------------------------
INSERT INTO "public"."associated_policy" VALUES ('e93d7769-c6bd-4916-a47d-bfb5ca7720a1', 'edfc1154-e705-44d5-b0dd-9bc2521bb603');
INSERT INTO "public"."associated_policy" VALUES ('cf8691be-3650-4730-8ab7-7cae73116405', '548f1a2e-ec47-4862-8195-49540ca2ad3b');
INSERT INTO "public"."associated_policy" VALUES ('14f0a671-d39f-4631-be9b-c82e698cad94', '69e0cbbc-8c1b-4ca0-aa8a-1baa48c3f766');
INSERT INTO "public"."associated_policy" VALUES ('9bf2df2c-3864-41c3-8a1e-91614452caa7', 'b816debf-bfbd-4096-82da-4df49f07047b');
INSERT INTO "public"."associated_policy" VALUES ('872936ed-cf13-4e72-8bae-8d1625c42929', '61b4bf1a-f4bb-43a0-b91f-bbb37b1ab203');
INSERT INTO "public"."associated_policy" VALUES ('0d93ce92-4576-46fa-9a42-e9ad4b6c77da', '97ad4b74-cde2-45ff-97d4-b411ac0a7153');
-- ----------------------------
-- Table structure for authentication_execution
-- ----------------------------
DROP TABLE IF EXISTS "public"."authentication_execution";
CREATE TABLE "public"."authentication_execution" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "alias" varchar(255) COLLATE "pg_catalog"."default",
  "authenticator" varchar(36) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "flow_id" varchar(36) COLLATE "pg_catalog"."default",
  "requirement" int4,
  "priority" int4,
  "authenticator_flow" bool NOT NULL DEFAULT false,
  "auth_flow_id" varchar(36) COLLATE "pg_catalog"."default",
  "auth_config" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of authentication_execution
-- ----------------------------
INSERT INTO "public"."authentication_execution" VALUES ('bd0336ff-ad88-49a2-9aa4-0e0e00d5494d', NULL, 'auth-cookie', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('de38e7ac-1c03-47b8-9590-275b4d777d95', NULL, 'auth-spnego', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', 3, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('b45b5e9b-0bb1-4d23-b833-e3747bf5da1e', NULL, 'identity-provider-redirector', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', 2, 25, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('5e4a7851-a6d0-4919-8a4b-95afcf9db244', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', 2, 30, 't', '6cc1961f-c833-44ef-ac8b-2fde3603a02a', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('c2eb017d-e3aa-4e5d-b962-f955d3a83585', NULL, 'auth-username-password-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '6cc1961f-c833-44ef-ac8b-2fde3603a02a', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('fadcec5f-f109-4c2d-bef0-a6fc11dc661e', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '6cc1961f-c833-44ef-ac8b-2fde3603a02a', 1, 20, 't', 'ef77314d-8135-4304-8bbe-d6bbb3337783', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('3342f77f-df1e-4eb6-8a27-7c35d063ce86', NULL, 'conditional-user-configured', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'ef77314d-8135-4304-8bbe-d6bbb3337783', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('84528113-dd4a-41dc-808e-29cd40aaa69b', NULL, 'auth-otp-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'ef77314d-8135-4304-8bbe-d6bbb3337783', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4fc576b1-4b16-4232-aa81-a705324a4f37', NULL, 'direct-grant-validate-username', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'd6da9d19-cb71-4841-9cfa-90a111d1b292', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('5a67fc7e-5ab4-4899-9a5f-fe3d674cdb6c', NULL, 'direct-grant-validate-password', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'd6da9d19-cb71-4841-9cfa-90a111d1b292', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('e4a3f37e-0cf5-422d-917d-81b48e38d7e5', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'd6da9d19-cb71-4841-9cfa-90a111d1b292', 1, 30, 't', '17c4d143-9c08-45d5-a182-2a7a9d8bb782', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('afbcd768-eaf8-4db6-8f9a-79ccd6731e6c', NULL, 'conditional-user-configured', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '17c4d143-9c08-45d5-a182-2a7a9d8bb782', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('bac27d75-2208-4d2b-8b07-4e4ab888aa41', NULL, 'direct-grant-validate-otp', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '17c4d143-9c08-45d5-a182-2a7a9d8bb782', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('fb304477-40ed-433e-9ccc-c36644006398', NULL, 'registration-page-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '6e0df28a-8860-4037-b9ca-0efcae55b8b4', 0, 10, 't', 'af02b3a3-fa00-4c92-9ffb-828bda4af8d0', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('3d0af7fb-b459-4a88-b961-5e937080cb85', NULL, 'registration-user-creation', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'af02b3a3-fa00-4c92-9ffb-828bda4af8d0', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('513b031f-3760-4c16-9478-ebe219be1b32', NULL, 'registration-password-action', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'af02b3a3-fa00-4c92-9ffb-828bda4af8d0', 0, 50, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('b812d751-9618-4d33-8f04-b4311b822700', NULL, 'registration-recaptcha-action', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'af02b3a3-fa00-4c92-9ffb-828bda4af8d0', 3, 60, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a10b635c-4c62-4bac-8c52-46f0b6a1f85c', NULL, 'registration-terms-and-conditions', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'af02b3a3-fa00-4c92-9ffb-828bda4af8d0', 3, 70, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('26e70af1-bac3-4156-bf50-5154afeaca48', NULL, 'reset-credentials-choose-user', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4759605b-ec2d-48ad-9754-49e4be8ba93a', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4f24ff94-fafb-450c-857e-7fb4a1a7e17c', NULL, 'reset-credential-email', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4759605b-ec2d-48ad-9754-49e4be8ba93a', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4db8bfc8-0ed5-4559-bd8a-46d11e3a7783', NULL, 'reset-password', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4759605b-ec2d-48ad-9754-49e4be8ba93a', 0, 30, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('fe276854-b257-4699-99b4-a59b9208b60b', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4759605b-ec2d-48ad-9754-49e4be8ba93a', 1, 40, 't', 'df941b9e-c22a-4d56-a59d-e51d977c624e', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('61adc751-1635-4426-8fbc-870452eaf50e', NULL, 'conditional-user-configured', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'df941b9e-c22a-4d56-a59d-e51d977c624e', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('aecbbf20-c319-48b8-bec4-aee3268c179d', NULL, 'reset-otp', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'df941b9e-c22a-4d56-a59d-e51d977c624e', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('ee95b644-5ec2-4b1c-b05c-fa651e90610d', NULL, 'client-secret', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('77d0f72b-4576-4645-97ee-8f14a22696a6', NULL, 'client-jwt', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 2, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('6702448c-a8cb-4864-9513-b82d465431fd', NULL, 'client-secret-jwt', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 2, 30, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('46dbe518-c305-4358-99b7-f29da4784b0b', NULL, 'client-x509', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 2, 40, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d44372f0-50b0-4437-a77a-97e3ef388db4', NULL, 'idp-review-profile', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3e2d7701-9174-4e6a-93fc-e63781247499', 0, 10, 'f', NULL, '8b5a8673-ff83-45b2-be6f-f67b9b18e76f');
INSERT INTO "public"."authentication_execution" VALUES ('013e37c2-f4c5-4870-8c5d-46777757f825', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3e2d7701-9174-4e6a-93fc-e63781247499', 0, 20, 't', '4d97247e-8f6f-498b-98be-5f7bdb012157', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d37cb174-e997-456b-add2-8c712bbd6ab5', NULL, 'idp-create-user-if-unique', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4d97247e-8f6f-498b-98be-5f7bdb012157', 2, 10, 'f', NULL, '835ae6e1-d85a-4543-8c69-3181858078f1');
INSERT INTO "public"."authentication_execution" VALUES ('36354b29-1b09-45cc-ab8c-1c87279e6041', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '4d97247e-8f6f-498b-98be-5f7bdb012157', 2, 20, 't', 'a7cbc050-c81b-4596-bd55-9fde23d2283c', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('ef569715-a6a1-4901-a089-d9f0bfca7e89', NULL, 'idp-confirm-link', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a7cbc050-c81b-4596-bd55-9fde23d2283c', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('da7f9280-d5b2-4678-af02-1436dabdf4cf', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'a7cbc050-c81b-4596-bd55-9fde23d2283c', 0, 20, 't', '18f97b8e-1853-4ea3-92e2-900c90f1e4c1', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('99a66612-8c03-4950-ad7f-9ae44c5ea11e', NULL, 'idp-email-verification', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18f97b8e-1853-4ea3-92e2-900c90f1e4c1', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('12dd3c9b-c920-44fc-b1ac-2bf60d582a1e', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18f97b8e-1853-4ea3-92e2-900c90f1e4c1', 2, 20, 't', '27309da0-de6d-45aa-8977-6b25b66fff9a', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d14eeff5-fab0-45d4-b105-438620b66174', NULL, 'idp-username-password-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '27309da0-de6d-45aa-8977-6b25b66fff9a', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('3333cbf6-84fc-4d78-8b90-5bc7079c4d14', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '27309da0-de6d-45aa-8977-6b25b66fff9a', 1, 20, 't', '5ea238a5-bb53-4db8-b89c-6af9691a8cf3', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9f52cf94-b984-4064-be32-5c817d8d70dc', NULL, 'conditional-user-configured', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '5ea238a5-bb53-4db8-b89c-6af9691a8cf3', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9a6f2997-32f4-4ea4-a055-1b77eae84054', NULL, 'auth-otp-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '5ea238a5-bb53-4db8-b89c-6af9691a8cf3', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('81e9295f-cc75-413c-b7db-51e71401350b', NULL, 'http-basic-authenticator', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'd14425e1-abbe-43ba-9e7f-48979d101681', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('84c1bacc-c396-4d08-a8f3-24cf2bd4d98c', NULL, 'docker-http-basic-authenticator', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3d8206dd-13aa-46b4-a0fc-444e5b9af4a6', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9f274b69-e836-4558-aa80-3dfcc810be4d', NULL, 'auth-cookie', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e007fedd-2171-44f9-826f-fa91f76ffe20', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('231d6da6-009f-4e13-a724-403dca812b50', NULL, 'auth-spnego', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e007fedd-2171-44f9-826f-fa91f76ffe20', 3, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('599ec4dc-5aff-45cb-842c-8dfd8790d5af', NULL, 'identity-provider-redirector', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e007fedd-2171-44f9-826f-fa91f76ffe20', 2, 25, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('efeaac4e-282d-47e1-9a24-d26cf3ff7f1b', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e007fedd-2171-44f9-826f-fa91f76ffe20', 2, 30, 't', '3b02ed6e-c6c7-4552-aad8-6f2d54b65ab4', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('339f95d4-763e-4985-a9e4-9c66f3d532fd', NULL, 'auth-username-password-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '3b02ed6e-c6c7-4552-aad8-6f2d54b65ab4', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('772206ee-0c38-4d53-ae97-64476fe1f475', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e007fedd-2171-44f9-826f-fa91f76ffe20', 2, 26, 't', '47578120-a547-402b-b22f-1970f4b257d1', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('c95dcce4-2c81-4a43-bd37-555938a01fd5', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '47578120-a547-402b-b22f-1970f4b257d1', 1, 10, 't', '4dc07f96-d5f6-41e4-ac8c-83614156f06e', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('2b57c746-c4ad-45b6-9df1-9be31adb013c', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4dc07f96-d5f6-41e4-ac8c-83614156f06e', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9c2ee370-46e4-480f-a8fe-59cc67596c14', NULL, 'organization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4dc07f96-d5f6-41e4-ac8c-83614156f06e', 2, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('7177d5d1-e40d-42d8-bf73-5eaf78582772', NULL, 'direct-grant-validate-username', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '13eaac88-bd3c-4309-ac4c-e7aef4b566da', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a31fb690-5008-4ae3-bc13-d03dfb538199', NULL, 'direct-grant-validate-password', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '13eaac88-bd3c-4309-ac4c-e7aef4b566da', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('cbdeec73-1346-4b90-8723-aadd034dd9e1', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '13eaac88-bd3c-4309-ac4c-e7aef4b566da', 1, 30, 't', '212a9bd8-cd40-4a58-8218-8ffd98184418', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('f5ef24b5-325e-4e0b-ab39-aa950de27920', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '212a9bd8-cd40-4a58-8218-8ffd98184418', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('0df0e57d-e804-4d7d-9c79-785886325f9b', NULL, 'direct-grant-validate-otp', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '212a9bd8-cd40-4a58-8218-8ffd98184418', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('01ecb108-bf65-4be0-905a-7453d9620a88', NULL, 'registration-page-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '88e45fd1-dc20-4df7-a4f7-47325ceb7e02', 0, 10, 't', '4842bbc9-fefa-493b-abb4-bc3a03987218', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('8caa0e62-5f51-4512-af56-1204cce3b24e', NULL, 'registration-user-creation', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4842bbc9-fefa-493b-abb4-bc3a03987218', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('46bc866a-f350-4ff7-95c1-bf9738f727bf', NULL, 'registration-password-action', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4842bbc9-fefa-493b-abb4-bc3a03987218', 0, 50, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('f96d9486-109e-4242-84f5-8ed728c4e197', NULL, 'registration-terms-and-conditions', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4842bbc9-fefa-493b-abb4-bc3a03987218', 3, 70, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('30bf067f-7f01-4ad0-a09c-e23c2c081b9e', NULL, 'reset-credentials-choose-user', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7f6ac3fc-86b5-48dc-9984-16b5b26b449b', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('7f666c8a-64fe-4b0d-b5b8-cc1cc4a89fd8', NULL, 'reset-credential-email', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7f6ac3fc-86b5-48dc-9984-16b5b26b449b', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a251f3e8-9dd3-4a7b-b5d8-fdfcae68b6f3', NULL, 'reset-password', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7f6ac3fc-86b5-48dc-9984-16b5b26b449b', 0, 30, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('b478be60-6f6a-4c96-9d2f-d6d2c08cdfda', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7f6ac3fc-86b5-48dc-9984-16b5b26b449b', 1, 40, 't', '32c95495-2e14-4bdc-aa8a-ca45f90e0bd0', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('3b3d4b36-59eb-48a5-995a-b5e4a23416b8', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '32c95495-2e14-4bdc-aa8a-ca45f90e0bd0', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4fc066c1-b1b2-4497-9354-947729e7ee92', NULL, 'reset-otp', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '32c95495-2e14-4bdc-aa8a-ca45f90e0bd0', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('b28f04b6-4fa2-475f-a25a-a23e1cd0bbe4', NULL, 'client-secret', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7b041d92-f8f9-46ac-81a0-a712cb0dc122', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a45cdc7a-1a57-406d-ba02-be0f1c05defc', NULL, 'client-jwt', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7b041d92-f8f9-46ac-81a0-a712cb0dc122', 2, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('f29a8cb6-75ba-44f7-b191-f0c3a896506d', NULL, 'client-secret-jwt', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7b041d92-f8f9-46ac-81a0-a712cb0dc122', 2, 30, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('eeddf0a0-cfa8-4e24-b553-18fdd8e70ee2', NULL, 'client-x509', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7b041d92-f8f9-46ac-81a0-a712cb0dc122', 2, 40, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4df6cd89-634e-463a-8e82-d17037c7491a', NULL, 'idp-review-profile', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '545cbde1-6116-461f-a031-ff051faf7c21', 0, 10, 'f', NULL, '9a77b5e7-cd9d-4b09-a8e5-494c982c6d13');
INSERT INTO "public"."authentication_execution" VALUES ('848da61b-1e62-4410-9cca-733301af42ab', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '545cbde1-6116-461f-a031-ff051faf7c21', 0, 20, 't', '49798430-f5b9-4fdd-b726-a5d6293aefca', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('3de3161a-6c3c-4260-9475-2a870ad073a5', NULL, 'idp-create-user-if-unique', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '49798430-f5b9-4fdd-b726-a5d6293aefca', 2, 10, 'f', NULL, 'd4547adf-724a-47de-91df-3c7813c12be0');
INSERT INTO "public"."authentication_execution" VALUES ('4831bc67-7d43-4761-b006-14ad7bf0c8cb', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '49798430-f5b9-4fdd-b726-a5d6293aefca', 2, 20, 't', 'c6a0266e-1dfb-49c0-884e-d422bdac7b30', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('8e6e6906-a86d-4027-a28f-da0be6756f74', NULL, 'idp-confirm-link', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'c6a0266e-1dfb-49c0-884e-d422bdac7b30', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('2908d8ad-4e9f-4197-bb38-b171bfe19e34', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'c6a0266e-1dfb-49c0-884e-d422bdac7b30', 0, 20, 't', '5b47772e-9a89-455c-9b0f-fb6db1a97b2c', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('e818153b-41db-453c-81a5-3dac67fcecf4', NULL, 'idp-email-verification', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5b47772e-9a89-455c-9b0f-fb6db1a97b2c', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('88b19a29-e428-44c2-a30b-e04b4ebc9d77', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5b47772e-9a89-455c-9b0f-fb6db1a97b2c', 2, 20, 't', 'b2e3c4e6-79a1-47eb-a005-1fc6e41366f6', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('bfb5e80e-2f8b-4f04-822e-64d679918744', NULL, 'idp-username-password-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'b2e3c4e6-79a1-47eb-a005-1fc6e41366f6', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9f8677fd-ff67-4b7c-8592-608592e77b73', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'b2e3c4e6-79a1-47eb-a005-1fc6e41366f6', 1, 20, 't', '0e54b0b4-7c4d-42c8-b5cc-b3ce3d08b59b', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('488d283e-f522-43de-a804-027709375481', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0e54b0b4-7c4d-42c8-b5cc-b3ce3d08b59b', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('2dbebc4b-f4fa-4289-8b8c-e1773bfc3fa9', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '3b02ed6e-c6c7-4552-aad8-6f2d54b65ab4', 1, 20, 't', 'c5414138-e484-4740-a22c-224af516c355', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('4221dadb-374d-4b08-945f-e9b0d84749c9', NULL, 'auth-otp-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'c5414138-e484-4740-a22c-224af516c355', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('1fde35c3-7911-4597-aaf9-5fb0f9f489a2', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'c5414138-e484-4740-a22c-224af516c355', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('dff70ea3-956c-4be3-8398-a6422fdd09f5', NULL, 'auth-otp-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0e54b0b4-7c4d-42c8-b5cc-b3ce3d08b59b', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9d99414a-d3c2-4df5-83d0-88ed07b94d7d', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '545cbde1-6116-461f-a031-ff051faf7c21', 1, 50, 't', '891cb307-1fc7-4d40-8a87-3f170a4d674d', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('19762bee-c539-4473-b5d5-7deddcb67de9', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '891cb307-1fc7-4d40-8a87-3f170a4d674d', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('eef794d6-6973-48f4-b62e-162c35c40bd7', NULL, 'idp-add-organization-member', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '891cb307-1fc7-4d40-8a87-3f170a4d674d', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('08dcb462-02a1-4cc8-ac9d-ec8dcd0f6302', NULL, 'http-basic-authenticator', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'fd46c425-dcd5-4896-aea0-5659e94c051f', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('0426b46a-ad22-4a6b-8116-ba8fedbcd254', NULL, 'docker-http-basic-authenticator', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '67631c78-793f-4605-98e3-be7756eba483', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('9bfd7696-010d-488b-9337-1ea65a5e766b', NULL, 'auth-cookie', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('87917b71-2025-4066-8821-095b21e16655', NULL, 'auth-spnego', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 3, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('b256d073-7112-4227-a4ce-fcabecff5c95', NULL, 'identity-provider-redirector', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 2, 25, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d96f7fbc-1810-4a08-9c5e-65d47d14ef31', NULL, 'auth-username-password-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9ad2eda6-45ec-4402-ac27-14eac681302f', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('2d1ea8b3-95ec-479c-b8f4-3536905aff60', NULL, 'conditional-user-configured', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3a762e84-cf7e-4dfa-a62f-1fd019b730a6', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('cb1ca81a-658f-42b4-982f-71903de7c7c9', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9ad2eda6-45ec-4402-ac27-14eac681302f', 1, 20, 't', '3a762e84-cf7e-4dfa-a62f-1fd019b730a6', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('03a0c045-a953-4257-beb5-02ae57c000ee', NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 2, 30, 't', '9ad2eda6-45ec-4402-ac27-14eac681302f', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('363de3fc-8e9b-4a9d-b0d6-f0178a5a46f8', NULL, 'http-basic-authenticator', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 3, 32, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a07f8336-a52a-4da3-9c01-1db731e0a1aa', NULL, 'auth-otp-form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '18326042-ea47-4afd-825b-6aa30a92f033', 3, 31, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('977bb2aa-ae2a-4f06-b309-35b91caad7f9', NULL, 'auth-cookie', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 2, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('e9ab513a-26da-4aa0-b94c-039caa4a1268', NULL, 'auth-spnego', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 3, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('28a9c2dd-3e59-4ebf-978d-5b83161fcc60', NULL, 'identity-provider-redirector', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 2, 25, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d92b3b06-e391-4a40-88f1-bf40f83ed428', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '661dee8b-9b0d-496d-98b5-953e4ec3c48e', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('d1fb358c-b929-45c6-ad82-4a912fc274f0', NULL, 'organization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '661dee8b-9b0d-496d-98b5-953e4ec3c48e', 2, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('40c3841b-0ccc-4d44-8d70-b1c981ecba90', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'b3a15fbd-6704-4f26-8803-e111988181df', 1, 10, 't', '661dee8b-9b0d-496d-98b5-953e4ec3c48e', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('c176516c-0a81-4ca8-9da5-04a11f0fd366', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 2, 26, 't', 'b3a15fbd-6704-4f26-8803-e111988181df', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('54798fe1-f07a-4d83-bb1d-cfa2b1154385', NULL, 'auth-username-password-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '18d25bfa-4606-4f8c-8323-994a7df8cedc', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('396d8b2a-6141-4dec-9ec0-c9f9b3a6ea49', NULL, 'conditional-user-configured', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4141298e-8c81-469e-b3f0-25a8d904c53d', 0, 10, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('7800ee97-8e78-4939-95d3-39c1909c11e3', NULL, 'auth-otp-form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4141298e-8c81-469e-b3f0-25a8d904c53d', 0, 20, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('8d88b17f-ba64-4179-aa92-220454b5a532', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '18d25bfa-4606-4f8c-8323-994a7df8cedc', 1, 20, 't', '4141298e-8c81-469e-b3f0-25a8d904c53d', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('5e5949c9-3c06-43ca-96f3-d1cb0977ce82', NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 2, 30, 't', '18d25bfa-4606-4f8c-8323-994a7df8cedc', NULL);
INSERT INTO "public"."authentication_execution" VALUES ('ef807d25-83c1-4cc3-a282-f67ae8742dda', NULL, 'http-basic-authenticator', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '613c2f38-7d02-4119-98bc-8c744b866fe7', 2, 31, 'f', NULL, NULL);
INSERT INTO "public"."authentication_execution" VALUES ('a97c8332-ed9d-438c-b28a-38537ab126f8', NULL, 'registration-recaptcha-action', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '4842bbc9-fefa-493b-abb4-bc3a03987218', 3, 60, 'f', NULL, NULL);

-- ----------------------------
-- Table structure for authentication_flow
-- ----------------------------
DROP TABLE IF EXISTS "public"."authentication_flow";
CREATE TABLE "public"."authentication_flow" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "alias" varchar(255) COLLATE "pg_catalog"."default",
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "provider_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'basic-flow'::character varying,
  "top_level" bool NOT NULL DEFAULT false,
  "built_in" bool NOT NULL DEFAULT false
)
;

-- ----------------------------
-- Records of authentication_flow
-- ----------------------------
INSERT INTO "public"."authentication_flow" VALUES ('a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', 'browser', 'Browser based authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('6cc1961f-c833-44ef-ac8b-2fde3603a02a', 'forms', 'Username, password, otp and other auth forms.', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('ef77314d-8135-4304-8bbe-d6bbb3337783', 'Browser - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('d6da9d19-cb71-4841-9cfa-90a111d1b292', 'direct grant', 'OpenID Connect Resource Owner Grant', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('17c4d143-9c08-45d5-a182-2a7a9d8bb782', 'Direct Grant - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('6e0df28a-8860-4037-b9ca-0efcae55b8b4', 'registration', 'Registration flow', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('af02b3a3-fa00-4c92-9ffb-828bda4af8d0', 'registration form', 'Registration form', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'form-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('4759605b-ec2d-48ad-9754-49e4be8ba93a', 'reset credentials', 'Reset credentials for a user if they forgot their password or something', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('df941b9e-c22a-4d56-a59d-e51d977c624e', 'Reset - Conditional OTP', 'Flow to determine if the OTP should be reset or not. Set to REQUIRED to force.', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 'clients', 'Base authentication for clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'client-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('3e2d7701-9174-4e6a-93fc-e63781247499', 'first broker login', 'Actions taken after first broker login with identity provider account, which is not yet linked to any Keycloak account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('4d97247e-8f6f-498b-98be-5f7bdb012157', 'User creation or linking', 'Flow for the existing/non-existing user alternatives', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('a7cbc050-c81b-4596-bd55-9fde23d2283c', 'Handle Existing Account', 'Handle what to do if there is existing account with same email/username like authenticated identity provider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('18f97b8e-1853-4ea3-92e2-900c90f1e4c1', 'Account verification options', 'Method with which to verity the existing account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('27309da0-de6d-45aa-8977-6b25b66fff9a', 'Verify Existing Account by Re-authentication', 'Reauthentication of existing account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('5ea238a5-bb53-4db8-b89c-6af9691a8cf3', 'First broker login - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('d14425e1-abbe-43ba-9e7f-48979d101681', 'saml ecp', 'SAML ECP Profile Authentication Flow', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('3d8206dd-13aa-46b4-a0fc-444e5b9af4a6', 'docker auth', 'Used by Docker clients to authenticate against the IDP', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('e007fedd-2171-44f9-826f-fa91f76ffe20', 'browser', 'Browser based authentication', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('3b02ed6e-c6c7-4552-aad8-6f2d54b65ab4', 'forms', 'Username, password, otp and other auth forms.', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('c5414138-e484-4740-a22c-224af516c355', 'Browser - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('47578120-a547-402b-b22f-1970f4b257d1', 'Organization', NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('4dc07f96-d5f6-41e4-ac8c-83614156f06e', 'Browser - Conditional Organization', 'Flow to determine if the organization identity-first login is to be used', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('13eaac88-bd3c-4309-ac4c-e7aef4b566da', 'direct grant', 'OpenID Connect Resource Owner Grant', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('212a9bd8-cd40-4a58-8218-8ffd98184418', 'Direct Grant - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('88e45fd1-dc20-4df7-a4f7-47325ceb7e02', 'registration', 'Registration flow', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('4842bbc9-fefa-493b-abb4-bc3a03987218', 'registration form', 'Registration form', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'form-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('7f6ac3fc-86b5-48dc-9984-16b5b26b449b', 'reset credentials', 'Reset credentials for a user if they forgot their password or something', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('32c95495-2e14-4bdc-aa8a-ca45f90e0bd0', 'Reset - Conditional OTP', 'Flow to determine if the OTP should be reset or not. Set to REQUIRED to force.', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('7b041d92-f8f9-46ac-81a0-a712cb0dc122', 'clients', 'Base authentication for clients', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'client-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('545cbde1-6116-461f-a031-ff051faf7c21', 'first broker login', 'Actions taken after first broker login with identity provider account, which is not yet linked to any Keycloak account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('49798430-f5b9-4fdd-b726-a5d6293aefca', 'User creation or linking', 'Flow for the existing/non-existing user alternatives', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('c6a0266e-1dfb-49c0-884e-d422bdac7b30', 'Handle Existing Account', 'Handle what to do if there is existing account with same email/username like authenticated identity provider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('5b47772e-9a89-455c-9b0f-fb6db1a97b2c', 'Account verification options', 'Method with which to verity the existing account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('b2e3c4e6-79a1-47eb-a005-1fc6e41366f6', 'Verify Existing Account by Re-authentication', 'Reauthentication of existing account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('0e54b0b4-7c4d-42c8-b5cc-b3ce3d08b59b', 'First broker login - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('891cb307-1fc7-4d40-8a87-3f170a4d674d', 'First Broker Login - Conditional Organization', 'Flow to determine if the authenticator that adds organization members is to be used', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 't');
INSERT INTO "public"."authentication_flow" VALUES ('fd46c425-dcd5-4896-aea0-5659e94c051f', 'saml ecp', 'SAML ECP Profile Authentication Flow', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('67631c78-793f-4605-98e3-be7756eba483', 'docker auth', 'Used by Docker clients to authenticate against the IDP', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 't');
INSERT INTO "public"."authentication_flow" VALUES ('18326042-ea47-4afd-825b-6aa30a92f033', 'Copy of browser', 'Browser based authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 't', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('9ad2eda6-45ec-4402-ac27-14eac681302f', 'Copy of browser forms', 'Username, password, otp and other auth forms.', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('3a762e84-cf7e-4dfa-a62f-1fd019b730a6', 'Copy of browser Browser - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('b3a15fbd-6704-4f26-8803-e111988181df', 'Copy of browser Organization', NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('661dee8b-9b0d-496d-98b5-953e4ec3c48e', 'Copy of browser Browser - Conditional Organization', 'Flow to determine if the organization identity-first login is to be used', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('18d25bfa-4606-4f8c-8323-994a7df8cedc', 'Copy of browser forms', 'Username, password, otp and other auth forms.', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('4141298e-8c81-469e-b3f0-25a8d904c53d', 'Copy of browser Browser - Conditional OTP', 'Flow to determine if the OTP is required for the authentication', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 'f', 'f');
INSERT INTO "public"."authentication_flow" VALUES ('613c2f38-7d02-4119-98bc-8c744b866fe7', 'Copy of browser', 'Browser based passwords', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'basic-flow', 't', 'f');

-- ----------------------------
-- Table structure for authenticator_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."authenticator_config";
CREATE TABLE "public"."authenticator_config" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "alias" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of authenticator_config
-- ----------------------------
INSERT INTO "public"."authenticator_config" VALUES ('8b5a8673-ff83-45b2-be6f-f67b9b18e76f', 'review profile config', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724');
INSERT INTO "public"."authenticator_config" VALUES ('835ae6e1-d85a-4543-8c69-3181858078f1', 'create unique user config', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724');
INSERT INTO "public"."authenticator_config" VALUES ('9a77b5e7-cd9d-4b09-a8e5-494c982c6d13', 'review profile config', '8920b375-d705-4d30-8a71-52d9c14ec4ba');
INSERT INTO "public"."authenticator_config" VALUES ('d4547adf-724a-47de-91df-3c7813c12be0', 'create unique user config', '8920b375-d705-4d30-8a71-52d9c14ec4ba');

-- ----------------------------
-- Table structure for authenticator_config_entry
-- ----------------------------
DROP TABLE IF EXISTS "public"."authenticator_config_entry";
CREATE TABLE "public"."authenticator_config_entry" (
  "authenticator_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of authenticator_config_entry
-- ----------------------------
INSERT INTO "public"."authenticator_config_entry" VALUES ('835ae6e1-d85a-4543-8c69-3181858078f1', 'false', 'require.password.update.after.registration');
INSERT INTO "public"."authenticator_config_entry" VALUES ('8b5a8673-ff83-45b2-be6f-f67b9b18e76f', 'missing', 'update.profile.on.first.login');
INSERT INTO "public"."authenticator_config_entry" VALUES ('9a77b5e7-cd9d-4b09-a8e5-494c982c6d13', 'missing', 'update.profile.on.first.login');
INSERT INTO "public"."authenticator_config_entry" VALUES ('d4547adf-724a-47de-91df-3c7813c12be0', 'false', 'require.password.update.after.registration');

-- ----------------------------
-- Table structure for broker_link
-- ----------------------------
DROP TABLE IF EXISTS "public"."broker_link";
CREATE TABLE "public"."broker_link" (
  "identity_provider" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "broker_user_id" varchar(255) COLLATE "pg_catalog"."default",
  "broker_username" varchar(255) COLLATE "pg_catalog"."default",
  "token" text COLLATE "pg_catalog"."default",
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of broker_link
-- ----------------------------

-- ----------------------------
-- Table structure for client
-- ----------------------------
DROP TABLE IF EXISTS "public"."client";
CREATE TABLE "public"."client" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "enabled" bool NOT NULL DEFAULT false,
  "full_scope_allowed" bool NOT NULL DEFAULT false,
  "client_id" varchar(255) COLLATE "pg_catalog"."default",
  "not_before" int4,
  "public_client" bool NOT NULL DEFAULT false,
  "secret" varchar(255) COLLATE "pg_catalog"."default",
  "base_url" varchar(255) COLLATE "pg_catalog"."default",
  "bearer_only" bool NOT NULL DEFAULT false,
  "management_url" varchar(255) COLLATE "pg_catalog"."default",
  "surrogate_auth_required" bool NOT NULL DEFAULT false,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "protocol" varchar(255) COLLATE "pg_catalog"."default",
  "node_rereg_timeout" int4 DEFAULT 0,
  "frontchannel_logout" bool NOT NULL DEFAULT false,
  "consent_required" bool NOT NULL DEFAULT false,
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "service_accounts_enabled" bool NOT NULL DEFAULT false,
  "client_authenticator_type" varchar(255) COLLATE "pg_catalog"."default",
  "root_url" varchar(255) COLLATE "pg_catalog"."default",
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "registration_token" varchar(255) COLLATE "pg_catalog"."default",
  "standard_flow_enabled" bool NOT NULL DEFAULT true,
  "implicit_flow_enabled" bool NOT NULL DEFAULT false,
  "direct_access_grants_enabled" bool NOT NULL DEFAULT false,
  "always_display_in_console" bool NOT NULL DEFAULT false
)
;

-- ----------------------------
-- Records of client
-- ----------------------------
INSERT INTO "public"."client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', 't', 'f', 'master-realm', 0, 'f', NULL, NULL, 't', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, 0, 'f', 'f', 'master Realm', 'f', 'client-secret', NULL, NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', 'f', 'account', 0, 't', NULL, '/realms/master/account/', 'f', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'openid-connect', 0, 'f', 'f', '${client_account}', 'f', 'client-secret', '${authBaseUrl}', NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', 't', 'f', 'account-console', 0, 't', NULL, '/realms/master/account/', 'f', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'openid-connect', 0, 'f', 'f', '${client_account-console}', 'f', 'client-secret', '${authBaseUrl}', NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', 't', 'f', 'broker', 0, 'f', NULL, NULL, 't', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'openid-connect', 0, 'f', 'f', '${client_broker}', 'f', 'client-secret', NULL, NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 't', 't', 'security-admin-console', 0, 't', NULL, '/admin/master/console/', 'f', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'openid-connect', 0, 'f', 'f', '${client_security-admin-console}', 'f', 'client-secret', '${authAdminUrl}', NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', 't', 't', 'admin-cli', 0, 't', NULL, NULL, 'f', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'openid-connect', 0, 'f', 'f', '${client_admin-cli}', 'f', 'client-secret', NULL, NULL, NULL, 'f', 'f', 't', 'f');
INSERT INTO "public"."client" VALUES ('c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', 'f', 'supos-realm', 0, 'f', NULL, NULL, 't', NULL, 'f', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, 0, 'f', 'f', 'supos Realm', 'f', 'client-secret', NULL, NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 't', 'f', 'realm-management', 0, 'f', NULL, NULL, 't', NULL, 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_realm-management}', 'f', 'client-secret', NULL, NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 't', 'f', 'broker', 0, 'f', NULL, NULL, 't', NULL, 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_broker}', 'f', 'client-secret', NULL, NULL, NULL, 't', 'f', 'f', 'f');
INSERT INTO "public"."client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', 'f', 'account', 0, 't', NULL, '/realms/supos/account/', 'f', '', 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_account}', 'f', 'client-secret', '${authBaseUrl}', '', NULL, 't', 't', 't', 'f');
INSERT INTO "public"."client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 't', 'f', 'account-console', 0, 't', NULL, '/realms/supos/account/', 'f', '', 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_account-console}', 'f', 'client-secret', '${authBaseUrl}', '', NULL, 't', 't', 't', 'f');
INSERT INTO "public"."client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 't', 't', 'admin-cli', 0, 't', NULL, '', 'f', '', 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_admin-cli}', 'f', 'client-secret', '', '', NULL, 't', 't', 't', 'f');
INSERT INTO "public"."client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 't', 't', 'security-admin-console', 0, 't', NULL, '/admin/supos/console/', 'f', '', 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', 0, 'f', 'f', '${client_security-admin-console}', 'f', 'client-secret', '${authAdminUrl}', '', NULL, 't', 't', 't', 'f');
INSERT INTO "public"."client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', 't', 'supos', 0, 'f', 'VaOS2makbDhJJsLlYPt4Wl87bo9VzXiO', 'KEYCLOAK_BASE_URL_VAR/home', 'f', 'KEYCLOAK_BASE_URL_VAR', 'f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'openid-connect', -1, 't', 'f', '', 't', 'client-secret', 'KEYCLOAK_BASE_URL_VAR', '', NULL, 't', 'f', 't', 'f');

-- ----------------------------
-- Table structure for client_attributes
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_attributes";
CREATE TABLE "public"."client_attributes" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of client_attributes
-- ----------------------------
INSERT INTO "public"."client_attributes" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', 'pkce.code.challenge.method', 'S256');
INSERT INTO "public"."client_attributes" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 'pkce.code.challenge.method', 'S256');
INSERT INTO "public"."client_attributes" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 'client.use.lightweight.access.token.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', 'client.use.lightweight.access.token.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'pkce.code.challenge.method', 'S256');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'post.logout.redirect.uris', '+');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'pkce.code.challenge.method', 'S256');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'client.use.lightweight.access.token.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'client.use.lightweight.access.token.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'oauth2.device.authorization.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'backchannel.logout.revoke.offline.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'realm_client', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'display.on.consent.screen', 'false');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'realm_client', 'false');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'oauth2.device.authorization.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'oidc.ciba.grant.enabled', 'false');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'display.on.consent.screen', 'false');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'backchannel.logout.session.required', 'true');
INSERT INTO "public"."client_attributes" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'backchannel.logout.revoke.offline.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'realm_client', 'false');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'oauth2.device.authorization.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'oidc.ciba.grant.enabled', 'false');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'display.on.consent.screen', 'false');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'backchannel.logout.session.required', 'true');
INSERT INTO "public"."client_attributes" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'backchannel.logout.revoke.offline.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'realm_client', 'false');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'oauth2.device.authorization.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'oidc.ciba.grant.enabled', 'false');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'display.on.consent.screen', 'false');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'backchannel.logout.session.required', 'true');
INSERT INTO "public"."client_attributes" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'backchannel.logout.revoke.offline.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'realm_client', 'false');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'oauth2.device.authorization.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'oidc.ciba.grant.enabled', 'false');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'display.on.consent.screen', 'false');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'backchannel.logout.session.required', 'true');
INSERT INTO "public"."client_attributes" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'backchannel.logout.revoke.offline.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'use.refresh.tokens', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'token.response.type.bearer.lower-case', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'tls.client.certificate.bound.access.tokens', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'require.pushed.authorization.requests', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'client.use.lightweight.access.token.enabled', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'client.introspection.response.allow.jwt.claim.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'acr.loa.map', '{}');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'client.secret.creation.time', '1729680416');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'use.jwks.url', 'false');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'backchannel.logout.session.required', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'oidc.ciba.grant.enabled', 'true');
INSERT INTO "public"."client_attributes" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'client_credentials.use_refresh_token', 'false');

-- ----------------------------
-- Table structure for client_auth_flow_bindings
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_auth_flow_bindings";
CREATE TABLE "public"."client_auth_flow_bindings" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "flow_id" varchar(36) COLLATE "pg_catalog"."default",
  "binding_name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of client_auth_flow_bindings
-- ----------------------------

-- ----------------------------
-- Table structure for client_initial_access
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_initial_access";
CREATE TABLE "public"."client_initial_access" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "timestamp" int4,
  "expiration" int4,
  "count" int4,
  "remaining_count" int4
)
;

-- ----------------------------
-- Records of client_initial_access
-- ----------------------------

-- ----------------------------
-- Table structure for client_node_registrations
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_node_registrations";
CREATE TABLE "public"."client_node_registrations" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" int4,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of client_node_registrations
-- ----------------------------

-- ----------------------------
-- Table structure for client_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_scope";
CREATE TABLE "public"."client_scope" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "protocol" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of client_scope
-- ----------------------------
INSERT INTO "public"."client_scope" VALUES ('53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'offline_access', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect built-in scope: offline_access', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('e760b92c-cc89-486a-893d-9c0d5f792170', 'role_list', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'SAML role list', 'saml');
INSERT INTO "public"."client_scope" VALUES ('fab7a1a9-abaa-4598-8a28-e426140649ee', 'saml_organization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'Organization Membership', 'saml');
INSERT INTO "public"."client_scope" VALUES ('37b9772b-4a05-4b69-8f26-9c1ef1062650', 'profile', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect built-in scope: profile', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('955bf38a-c128-41db-b0b5-780cb1b6376d', 'email', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect built-in scope: email', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'address', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect built-in scope: address', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'phone', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect built-in scope: phone', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('0127c5fb-e845-40dc-b964-70639f5105d9', 'roles', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect scope for add user roles to the access token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('1da7a744-af7f-4da8-86d6-c4a8f49bed77', 'web-origins', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect scope for add allowed web origins to the access token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('19990782-99f3-4f22-a383-0c4832e4f781', 'microprofile-jwt', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'Microprofile - JWT built-in scope', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 'acr', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect scope for add acr (authentication context class reference) to the token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('8b71be46-e97e-4fb6-8044-ab9e91be71b2', 'basic', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'OpenID Connect scope for add all basic claims to the token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'organization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'Additional claims about the organization a subject belongs to', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('27d726f0-a771-44c8-8db7-e7a44329b42f', 'offline_access', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect built-in scope: offline_access', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('0dc62ecf-9c4c-40fb-ba9a-4725abf1d3ff', 'role_list', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SAML role list', 'saml');
INSERT INTO "public"."client_scope" VALUES ('f7db029c-7313-47c1-a6e8-56050682927f', 'saml_organization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'Organization Membership', 'saml');
INSERT INTO "public"."client_scope" VALUES ('19b9d4dd-3607-4e3a-838e-b156630fe78e', 'profile', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect built-in scope: profile', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('aab92fd1-d7b8-456c-aa9f-19c6c782260c', 'email', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect built-in scope: email', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'address', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect built-in scope: address', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('ad9286f6-2377-4db7-872b-5edcbef2017a', 'roles', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect scope for add user roles to the access token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('147d0a04-66fc-49db-a1c4-fa233eb47825', 'web-origins', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect scope for add allowed web origins to the access token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'microprofile-jwt', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'Microprofile - JWT built-in scope', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('80edc885-da5f-472c-90cc-d8b0e6d1f011', 'acr', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect scope for add acr (authentication context class reference) to the token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('9475e044-78d6-41ac-88a8-0cc0cedf5875', 'basic', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OpenID Connect scope for add all basic claims to the token', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('e5d6dd73-37ab-4864-abd8-b473bc110772', 'organization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'Additional claims about the organization a subject belongs to', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', 'firstTimeLogin', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'firstTimeLogin', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('4e44f85d-bb73-4eb9-af2a-c1a641792a94', 'tipsEnable', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'tipsEnable', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('51ec41ba-ea8b-4359-80a3-e3de154ee389', 'homePage', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'homePage', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', 'phone', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'phone', 'openid-connect');
INSERT INTO "public"."client_scope" VALUES ('804408e1-e065-4362-8cd1-414c9b9777b3', 'source', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'source', 'openid-connect');

-- ----------------------------
-- Table structure for client_scope_attributes
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_scope_attributes";
CREATE TABLE "public"."client_scope_attributes" (
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(2048) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of client_scope_attributes
-- ----------------------------
INSERT INTO "public"."client_scope_attributes" VALUES ('53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('53331029-d6f6-4c1e-a44a-b651c6b7f35a', '${offlineAccessScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('e760b92c-cc89-486a-893d-9c0d5f792170', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('e760b92c-cc89-486a-893d-9c0d5f792170', '${samlRoleListScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('fab7a1a9-abaa-4598-8a28-e426140649ee', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('37b9772b-4a05-4b69-8f26-9c1ef1062650', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('37b9772b-4a05-4b69-8f26-9c1ef1062650', '${profileScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('37b9772b-4a05-4b69-8f26-9c1ef1062650', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('955bf38a-c128-41db-b0b5-780cb1b6376d', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('955bf38a-c128-41db-b0b5-780cb1b6376d', '${emailScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('955bf38a-c128-41db-b0b5-780cb1b6376d', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', '${addressScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('0c85b1c7-6314-4a0b-b5fb-b76308dbab56', '${phoneScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('0127c5fb-e845-40dc-b964-70639f5105d9', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('0127c5fb-e845-40dc-b964-70639f5105d9', '${rolesScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('0127c5fb-e845-40dc-b964-70639f5105d9', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('1da7a744-af7f-4da8-86d6-c4a8f49bed77', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('1da7a744-af7f-4da8-86d6-c4a8f49bed77', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('1da7a744-af7f-4da8-86d6-c4a8f49bed77', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('19990782-99f3-4f22-a383-0c4832e4f781', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('19990782-99f3-4f22-a383-0c4832e4f781', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('8b71be46-e97e-4fb6-8044-ab9e91be71b2', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('8b71be46-e97e-4fb6-8044-ab9e91be71b2', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('8a1ce846-12c2-43ab-b220-93dcf015f2b6', '${organizationScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('27d726f0-a771-44c8-8db7-e7a44329b42f', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('27d726f0-a771-44c8-8db7-e7a44329b42f', '${offlineAccessScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('0dc62ecf-9c4c-40fb-ba9a-4725abf1d3ff', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('0dc62ecf-9c4c-40fb-ba9a-4725abf1d3ff', '${samlRoleListScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('f7db029c-7313-47c1-a6e8-56050682927f', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('19b9d4dd-3607-4e3a-838e-b156630fe78e', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('19b9d4dd-3607-4e3a-838e-b156630fe78e', '${profileScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('19b9d4dd-3607-4e3a-838e-b156630fe78e', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('aab92fd1-d7b8-456c-aa9f-19c6c782260c', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('aab92fd1-d7b8-456c-aa9f-19c6c782260c', '${emailScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('aab92fd1-d7b8-456c-aa9f-19c6c782260c', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('cca2f7fe-1d61-468e-a9df-83d25f108dc2', '${addressScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('ad9286f6-2377-4db7-872b-5edcbef2017a', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('ad9286f6-2377-4db7-872b-5edcbef2017a', '${rolesScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('ad9286f6-2377-4db7-872b-5edcbef2017a', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('147d0a04-66fc-49db-a1c4-fa233eb47825', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('147d0a04-66fc-49db-a1c4-fa233eb47825', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('147d0a04-66fc-49db-a1c4-fa233eb47825', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('80edc885-da5f-472c-90cc-d8b0e6d1f011', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('80edc885-da5f-472c-90cc-d8b0e6d1f011', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('9475e044-78d6-41ac-88a8-0cc0cedf5875', 'false', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('9475e044-78d6-41ac-88a8-0cc0cedf5875', 'false', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('e5d6dd73-37ab-4864-abd8-b473bc110772', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('e5d6dd73-37ab-4864-abd8-b473bc110772', '${organizationScopeConsentText}', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('e5d6dd73-37ab-4864-abd8-b473bc110772', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', '', 'gui.order');
INSERT INTO "public"."client_scope_attributes" VALUES ('4e44f85d-bb73-4eb9-af2a-c1a641792a94', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('4e44f85d-bb73-4eb9-af2a-c1a641792a94', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('4e44f85d-bb73-4eb9-af2a-c1a641792a94', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('4e44f85d-bb73-4eb9-af2a-c1a641792a94', '', 'gui.order');
INSERT INTO "public"."client_scope_attributes" VALUES ('51ec41ba-ea8b-4359-80a3-e3de154ee389', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('51ec41ba-ea8b-4359-80a3-e3de154ee389', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('51ec41ba-ea8b-4359-80a3-e3de154ee389', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('51ec41ba-ea8b-4359-80a3-e3de154ee389', '', 'gui.order');
INSERT INTO "public"."client_scope_attributes" VALUES ('7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', '', 'gui.order');
INSERT INTO "public"."client_scope_attributes" VALUES ('804408e1-e065-4362-8cd1-414c9b9777b3', 'true', 'display.on.consent.screen');
INSERT INTO "public"."client_scope_attributes" VALUES ('804408e1-e065-4362-8cd1-414c9b9777b3', '', 'consent.screen.text');
INSERT INTO "public"."client_scope_attributes" VALUES ('804408e1-e065-4362-8cd1-414c9b9777b3', 'true', 'include.in.token.scope');
INSERT INTO "public"."client_scope_attributes" VALUES ('804408e1-e065-4362-8cd1-414c9b9777b3', '', 'gui.order');

-- ----------------------------
-- Table structure for client_scope_client
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_scope_client";
CREATE TABLE "public"."client_scope_client" (
  "client_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "default_scope" bool NOT NULL DEFAULT false
)
;

-- ----------------------------
-- Records of client_scope_client
-- ----------------------------
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('fdc3b9b1-1fc5-4277-80ec-083f1a981eb3', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('b478982d-7f85-498c-8b83-2903d6c1116a', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('ad3bceb3-d55d-4588-a11a-71a0db1bd252', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('5b6e7278-a3a8-407d-94eb-6befd126bf16', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('1e143276-845b-4159-ad6b-1817ec62204c', '56c1640c-33d7-4e5f-a255-e920feab1200', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '4e44f85d-bb73-4eb9-af2a-c1a641792a94', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '51ec41ba-ea8b-4359-80a3-e3de154ee389', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', 't');
INSERT INTO "public"."client_scope_client" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '804408e1-e065-4362-8cd1-414c9b9777b3', 't');

-- ----------------------------
-- Table structure for client_scope_role_mapping
-- ----------------------------
DROP TABLE IF EXISTS "public"."client_scope_role_mapping";
CREATE TABLE "public"."client_scope_role_mapping" (
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "role_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of client_scope_role_mapping
-- ----------------------------
INSERT INTO "public"."client_scope_role_mapping" VALUES ('53331029-d6f6-4c1e-a44a-b651c6b7f35a', '80b2ebd4-bd1e-4449-b497-535ba5838c40');
INSERT INTO "public"."client_scope_role_mapping" VALUES ('27d726f0-a771-44c8-8db7-e7a44329b42f', 'c4cc1aab-5a86-4697-a33e-4976807b8563');

-- ----------------------------
-- Table structure for component
-- ----------------------------
DROP TABLE IF EXISTS "public"."component";
CREATE TABLE "public"."component" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "parent_id" varchar(36) COLLATE "pg_catalog"."default",
  "provider_id" varchar(36) COLLATE "pg_catalog"."default",
  "provider_type" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "sub_type" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of component
-- ----------------------------
INSERT INTO "public"."component" VALUES ('bf0b245d-fd46-4b9d-92b3-d0a4f3d8e0e9', 'Trusted Hosts', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'trusted-hosts', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('58d865f8-4a5c-4bff-aec6-f08ca720780d', 'Consent Required', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'consent-required', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('1525f611-da61-4b4a-b4e5-d7b48f3d9ecd', 'Full Scope Disabled', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'scope', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('1e863a6f-2558-4e15-be58-f3d8f62729eb', 'Max Clients Limit', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'max-clients', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'Allowed Protocol Mapper Types', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'allowed-protocol-mappers', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('183a1de8-a0fc-4c15-99b6-fdc989436533', 'Allowed Client Scopes', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'allowed-client-templates', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'anonymous');
INSERT INTO "public"."component" VALUES ('438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'Allowed Protocol Mapper Types', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'allowed-protocol-mappers', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'authenticated');
INSERT INTO "public"."component" VALUES ('8f35b765-ca94-42aa-b3b0-49df73432167', 'Allowed Client Scopes', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'allowed-client-templates', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'authenticated');
INSERT INTO "public"."component" VALUES ('3347fe25-a6c8-4701-900f-3ed5fb8162a7', 'rsa-generated', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'rsa-generated', 'org.keycloak.keys.KeyProvider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL);
INSERT INTO "public"."component" VALUES ('7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'rsa-enc-generated', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'rsa-enc-generated', 'org.keycloak.keys.KeyProvider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL);
INSERT INTO "public"."component" VALUES ('f5a178cb-322a-4697-8834-b91582067aab', 'hmac-generated-hs512', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'hmac-generated', 'org.keycloak.keys.KeyProvider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL);
INSERT INTO "public"."component" VALUES ('38df7ac0-5cdc-49db-bf07-56fb3c5e8a9b', 'aes-generated', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'aes-generated', 'org.keycloak.keys.KeyProvider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL);
INSERT INTO "public"."component" VALUES ('44cbce4a-4909-4ef3-908e-7eb1e06ca3e1', NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'declarative-user-profile', 'org.keycloak.userprofile.UserProfileProvider', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL);
INSERT INTO "public"."component" VALUES ('775f6650-1e03-472d-a3e0-004d863c2b40', 'rsa-generated', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'rsa-generated', 'org.keycloak.keys.KeyProvider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL);
INSERT INTO "public"."component" VALUES ('58f752c6-11ed-4224-b4ab-bd45d3c73654', 'rsa-enc-generated', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'rsa-enc-generated', 'org.keycloak.keys.KeyProvider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL);
INSERT INTO "public"."component" VALUES ('16d7a25b-a022-4cfb-8176-1059aeff83e8', 'hmac-generated-hs512', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'hmac-generated', 'org.keycloak.keys.KeyProvider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL);
INSERT INTO "public"."component" VALUES ('aeb804f9-595a-4394-b337-020f73bdc389', 'aes-generated', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'aes-generated', 'org.keycloak.keys.KeyProvider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL);
INSERT INTO "public"."component" VALUES ('3ee5ecda-1ea7-47b9-96ad-efc0280be683', 'Trusted Hosts', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'trusted-hosts', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('81b7d1db-f843-4df1-893e-115eb4f00c67', 'Consent Required', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'consent-required', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('325ac732-b1ce-4f49-9616-9d9cdd4df8ec', 'Full Scope Disabled', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'scope', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('988f193c-ecca-4580-ab7c-a79d701ae109', 'Max Clients Limit', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'max-clients', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('42125d59-532f-4f8b-bf8e-d87f7c55c080', 'Allowed Protocol Mapper Types', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'allowed-protocol-mappers', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('724be55b-51c3-45be-bf71-a5d7ae45b7ef', 'Allowed Client Scopes', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'allowed-client-templates', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'anonymous');
INSERT INTO "public"."component" VALUES ('bbfb659a-6fa6-49ea-a818-926b58d6fada', 'Allowed Protocol Mapper Types', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'allowed-protocol-mappers', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'authenticated');
INSERT INTO "public"."component" VALUES ('00d3022f-42ff-429c-add3-6083e760884e', 'Allowed Client Scopes', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'allowed-client-templates', 'org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'authenticated');
INSERT INTO "public"."component" VALUES ('a2cf44c3-ba30-4e9d-93e2-973943bb60a3', NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'declarative-user-profile', 'org.keycloak.userprofile.UserProfileProvider', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL);

-- ----------------------------
-- Table structure for component_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."component_config";
CREATE TABLE "public"."component_config" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "component_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of component_config
-- ----------------------------
INSERT INTO "public"."component_config" VALUES ('d938a180-2775-4acd-b51d-d9a3a1f7f42a', '1e863a6f-2558-4e15-be58-f3d8f62729eb', 'max-clients', '200');
INSERT INTO "public"."component_config" VALUES ('efa9afe3-969b-4b25-a285-5029df76b205', 'bf0b245d-fd46-4b9d-92b3-d0a4f3d8e0e9', 'host-sending-registration-request-must-match', 'true');
INSERT INTO "public"."component_config" VALUES ('e3e4cd29-17d1-448e-8842-f31d42a91e76', 'bf0b245d-fd46-4b9d-92b3-d0a4f3d8e0e9', 'client-uris-must-match', 'true');
INSERT INTO "public"."component_config" VALUES ('e7913ced-421a-44d3-9acb-07f7d7f81ef8', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'oidc-usermodel-property-mapper');
INSERT INTO "public"."component_config" VALUES ('7c4dc576-a3d1-4d27-8f50-9781bc5ab4a6', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'oidc-sha256-pairwise-sub-mapper');
INSERT INTO "public"."component_config" VALUES ('921cf66d-0e0b-43e5-b52a-6f6f79bfd45b', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'saml-role-list-mapper');
INSERT INTO "public"."component_config" VALUES ('3931ee94-d0b1-467f-ac7e-bbc6645db345', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'saml-user-property-mapper');
INSERT INTO "public"."component_config" VALUES ('3c54a8ef-0286-47dc-b1af-2772e9dd8d3d', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'oidc-usermodel-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('4c8339ce-cd8b-415b-a917-94eed57ca605', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'oidc-full-name-mapper');
INSERT INTO "public"."component_config" VALUES ('53723662-6e4f-4189-91e1-bcd0749424ce', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'saml-user-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('3220c7c5-3467-4d5f-a312-08a5dac6e1f3', '438c4eb5-8e96-4646-8569-fb3fd7f86f09', 'allowed-protocol-mapper-types', 'oidc-address-mapper');
INSERT INTO "public"."component_config" VALUES ('174a9b19-f151-4c8e-bb27-b497a22bc586', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'saml-user-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('bf389198-6166-4319-8cce-14130b7e498b', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'oidc-usermodel-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('3ae976d8-a1f2-45d4-ad7f-f03839990a57', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'oidc-address-mapper');
INSERT INTO "public"."component_config" VALUES ('f1eefb16-a475-4f2f-a0c3-898050db3621', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'saml-user-property-mapper');
INSERT INTO "public"."component_config" VALUES ('cbe2252b-bab2-4a37-94f4-c3144089743a', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'saml-role-list-mapper');
INSERT INTO "public"."component_config" VALUES ('cc8979c1-63cd-4c5a-8da5-1e2f1cee0adf', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'oidc-usermodel-property-mapper');
INSERT INTO "public"."component_config" VALUES ('c183fd42-262f-4131-b73a-3faacaa50e6f', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'oidc-full-name-mapper');
INSERT INTO "public"."component_config" VALUES ('3b6bb32d-595f-41de-82ac-3e5b1cc201d9', 'a7ef7e3c-9982-4a84-a6b0-dc6444c974e6', 'allowed-protocol-mapper-types', 'oidc-sha256-pairwise-sub-mapper');
INSERT INTO "public"."component_config" VALUES ('49acfc81-05b6-4ad7-809e-0cba33ae85d7', '8f35b765-ca94-42aa-b3b0-49df73432167', 'allow-default-scopes', 'true');
INSERT INTO "public"."component_config" VALUES ('36c5ae9e-5671-4ea8-84ed-97555bc67210', '183a1de8-a0fc-4c15-99b6-fdc989436533', 'allow-default-scopes', 'true');
INSERT INTO "public"."component_config" VALUES ('bd7c6c94-bd5b-44b5-b090-1ad0e6c493ce', '3347fe25-a6c8-4701-900f-3ed5fb8162a7', 'certificate', 'MIICmzCCAYMCBgGSuPWF6jANBgkqhkiG9w0BAQsFADARMQ8wDQYDVQQDDAZtYXN0ZXIwHhcNMjQxMDIzMTAzNzMxWhcNMzQxMDIzMTAzOTExWjARMQ8wDQYDVQQDDAZtYXN0ZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCnxRgFBlQb29P4HWhR5YxU+4tyXQU1EPcFUde02DcyqT5oUfizEGGKY3xmquQYITvBvYDlzbIpzvmin9nx/fhRmzkmZVg2ZuvkchjF+DastteHs3So2Z5nKXBa1XnUye7eA9n9sKPBiqrkmMJRvGS656RsprKlERz9hM037NQPw9QlsLDvbGSwJ6pNOk5JyDIkuGy5iwWZBoYeclW2YQidqSdw1psCuSJ0LfAFJQjdTLRSwJh0B6HV8dxhm36XXymF/KxuNVTiYsb8F/NrG2m8aL/1e9MVFuC1uPEAkY3fD0HckDxaV5XktBA2/XblgNG4hHSSgA4JDV9piyLnpXJnAgMBAAEwDQYJKoZIhvcNAQELBQADggEBAHkCoLbQiqxWQXTbOIDwkh8ioL3wcWL+uXogPQz5h6On4Y+lKW6haMXa2WZRR27nUQSDH2j/fRpW4BfV7wAFURBL2iolW1nJkshH9BSUhsom/V2q8pDZcE1Q7G7ba/A3XRKpqwKiqilV+DvPGafXcUAQjnOic6XPF/3NrSxU/Q8dIBEPgEfow+5PG299P7kTv4XTouVzkTNmwYFCkABGPToM5ZcU9tcm0BxDylo1ez605eJ5JZLNx0uIq3wrgWFmvKmTOAmxIY3fogdsp2dlLrSVA8T8N+tRCVh1uNY/A0TvAJZqMBnkJ0QQ5zZPpFZQ3FO+9qmahl8PgRkcP/SB6aU=');
INSERT INTO "public"."component_config" VALUES ('bb9d5da1-48ed-431e-a1d4-9b20e7aa0b4e', '3347fe25-a6c8-4701-900f-3ed5fb8162a7', 'privateKey', 'MIIEpAIBAAKCAQEAp8UYBQZUG9vT+B1oUeWMVPuLcl0FNRD3BVHXtNg3Mqk+aFH4sxBhimN8ZqrkGCE7wb2A5c2yKc75op/Z8f34UZs5JmVYNmbr5HIYxfg2rLbXh7N0qNmeZylwWtV51Mnu3gPZ/bCjwYqq5JjCUbxkuuekbKaypREc/YTNN+zUD8PUJbCw72xksCeqTTpOScgyJLhsuYsFmQaGHnJVtmEInakncNabArkidC3wBSUI3Uy0UsCYdAeh1fHcYZt+l18phfysbjVU4mLG/BfzaxtpvGi/9XvTFRbgtbjxAJGN3w9B3JA8WleV5LQQNv125YDRuIR0koAOCQ1faYsi56VyZwIDAQABAoIBABCa3tY8ep2pQ9EWZAlbDWkp3nLqxwWqELDrmUxybAAtJRqhJlrev7C5SQpGVr4Wp/n0fioAkmY18JpKdQFRED9PfDgTJsh3plhdfJs3hn3QuQNa3SyLIXT3coHjCCRp+iTqP6yuy35v8ZsfjXgWi+e93pXMZ/kTeQmUl9U1sQWARJsYY1moPsA38NuhrWTpHe10EsJz3JIsM8FDo3pglXEaWo4/NleXOIKMEYqvo4Z0TjFp+UcABz2WAEcBVTyOhD85VLwrzOFoyOIkvqoAm1nX9O2WH+vTvZ5fA8ntfwJMHPgmSuSLI51S2Pmwnf2x/cbpkiARUB9pIPZ9J2cUUXUCgYEA57CtcbAz4kya2U+sA5t27G+Cb2ZoogP/6IqxofbpPK9HZX8NugW0zjJH7unwNwUJ3hhdzkvIl9dyQTQ5NUM0A/9imlBrfUxpYTrIohYCeeMKj+XIy3B5R0UroG/Fx7SKkjrqwI+tDivAsf2QT7u+ZJ3+KnNohdh0oGWyaE5FUSsCgYEAuV974Pqu6aeVOEB9YRgvpzUQF91wsJSAgsznkTG9svYQ8YmF65fIWw4JSLBIwwBszcD65/ScpO94zbbIUVhIZimdkK+EdTUEnTptf1xFBuE6t17tKfD/LsHO9ggSj1XeFksbCHoN66dAWwarL/Or1Sk7XFl8NCgZ9vlYCFvbrbUCgYBbbcckJAp1dRFuTBhvW/w0FVT9rQYBWV61X3X1mkA0KF8eWGMMU5AkBoeIalzW+XAJasgpzpIcCXMW9ArXT+vI4BEDIFUqnuq+6bme7NyRSN00J9NzJLFXRJ6Qs0rzIfXE+ucEki4Sd4WVN6CpNkdN4WMZUW2f0+lCGp+qtah4/QKBgQCwi4+fpSAQx9oFyOWgIzi/NnotQGiiw8vgxuWEEqtoVZGteBxjVBstHSEaaUni7fSxwKk1YHIPY0LaKMp/LmVFZzz5HzA3sFbEp3DBa08STk9tdKrK1wsxrMM+7lE+0bjB4qXMXPIFDTTtfFxtPtTYHny6Zz8zLT4NSUveKh3+QQKBgQCjr8Dq7yl1dx9vfXwHF1j5kyIQfReroWCz5YJfkichNtNkD+ldwHOl48+QoVLOabL7qp9kIERW8u/O5MCMGqCZOTNvgkMWfGQLTThKzDmLEqDvZ1RyEp5/o4rR6RAYZ/Rzn1Z89wzfc0Xy62zq4EHOJsA8BaxTNUxIHE+llG6Zcg==');
INSERT INTO "public"."component_config" VALUES ('0b045e69-adfb-4ff7-872c-02e76235e1e1', '3347fe25-a6c8-4701-900f-3ed5fb8162a7', 'keyUse', 'SIG');
INSERT INTO "public"."component_config" VALUES ('893885cd-0e74-4fdb-a14d-e34395e96f2e', '3347fe25-a6c8-4701-900f-3ed5fb8162a7', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('f3f30c74-2067-427e-b8d0-44fe1390f506', '38df7ac0-5cdc-49db-bf07-56fb3c5e8a9b', 'secret', 'PaeSalAu6lQyXY8KpPE8fw');
INSERT INTO "public"."component_config" VALUES ('0771ff8d-a3b5-4df8-adcb-966196db9075', '38df7ac0-5cdc-49db-bf07-56fb3c5e8a9b', 'kid', 'fe40f157-968c-4aab-8cc4-1c669f1f662e');
INSERT INTO "public"."component_config" VALUES ('64fb451c-5f72-4722-aaea-25d00fd341ec', '38df7ac0-5cdc-49db-bf07-56fb3c5e8a9b', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('ad27c3b2-867a-439b-a7d0-a9a74bd8aacd', '7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('4938d0eb-8a33-4a9f-98be-800770840506', '7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'algorithm', 'RSA-OAEP');
INSERT INTO "public"."component_config" VALUES ('d77d4a33-e943-47be-b447-09e51c97f6be', '7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'certificate', 'MIICmzCCAYMCBgGSuPWGnzANBgkqhkiG9w0BAQsFADARMQ8wDQYDVQQDDAZtYXN0ZXIwHhcNMjQxMDIzMTAzNzMxWhcNMzQxMDIzMTAzOTExWjARMQ8wDQYDVQQDDAZtYXN0ZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCzsDDrf+uuO/eE1u8XYzzGhqoKlfsL+AXquypWsikdBwRZUo2157depjRoiakcCq3CVxrFkHZRGcKFLCmYBRjWWy8vjUwIARdlzsdFwVXxFrUMvh3zecbcaoReuLez1TStDewsbIB0B8sNtZKd1oyvabOclsxcdBJey5MjdiQAqYNRVFwFGjtVmilpHAcAftbad989fsXQBca5fWOBngeAWhSYIWsLS2gWmKTCQLkjqlqhvp5V8su3zlQBYbNEZb/qGhqIhAunixHYlDiQ7PpATTD2g/QAxrPyKyD6dj6EJ6fz8AEOP8lug7eXi5w5GC6fgC5U8OegKcOG1igvElQrAgMBAAEwDQYJKoZIhvcNAQELBQADggEBAGI+1rTMwPIbisQUC+wxZVs/eeIK6JZ3LLpQKXT/G5PIlj+9W/SwHN/ppHc+ck6ZGDHSrKXGqsl5ZA+OsnTG3QvMQd4jhbkCIcMAzsHRm1uEjJTghkbxAf5UPbn6U9TlBY8O/WIptXEG0v8BlkEZQGmZmD5tdJ6zMBEfThMN/NNFpE/bzsaiUsigy3S5zptXOkyihh3JWWP77c1JFyAyGG2HQdpE+29pPeoc+9pVNpWSfyV0QF2IJWLER/sqleZsDsq2emvhmaaSqQrkH3z8sCyvueLQ4OAAIyTsyo793f9aYmk/kyKhyhHtAPvFtgXVkBAhS2xr/yU+PG3+q9Kgj0U=');
INSERT INTO "public"."component_config" VALUES ('7f6345a0-de7d-4870-afbb-121115b0a0f0', '7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'privateKey', 'MIIEowIBAAKCAQEAs7Aw63/rrjv3hNbvF2M8xoaqCpX7C/gF6rsqVrIpHQcEWVKNtee3XqY0aImpHAqtwlcaxZB2URnChSwpmAUY1lsvL41MCAEXZc7HRcFV8Ra1DL4d83nG3GqEXri3s9U0rQ3sLGyAdAfLDbWSndaMr2mznJbMXHQSXsuTI3YkAKmDUVRcBRo7VZopaRwHAH7W2nffPX7F0AXGuX1jgZ4HgFoUmCFrC0toFpikwkC5I6paob6eVfLLt85UAWGzRGW/6hoaiIQLp4sR2JQ4kOz6QE0w9oP0AMaz8isg+nY+hCen8/ABDj/JboO3l4ucORgun4AuVPDnoCnDhtYoLxJUKwIDAQABAoIBABlknU0yiy5YuB50N16RPh36Gt6bGlqzJrbo009kJw48lx8+XNtnjxDXoRFi3tyhH7QWlih0RVwprUUfnBMpKTzlrvHj4GpDKTjQc3XGoCwNXvGZjmcBfxpYDTPLm95Uk3ifPpB6lt6O2WGrFriqgMArSmAnKWMpXg06bKU2xVi4XxrvAON+82V3+d7cwGxeLbmMZU0GiYpxTSkD09P047d9L88kJE575F0sSnUy3Aceb5vjXGPg7Cpc8mIuveLjmgP4x5TNwK1WaJspgLGz1fTjGqb/xLb6LkDMTe6enMWnh6woTNIqEfRccZfdZPJk2btaQShPt5fExagFmodhOmUCgYEA5hvji3ITUGnibCRr/oOgdyqiOR4x9BjIQGy+q6urwLG4ID2Vn7dFfrQPu3jFRBghLjFe9Whw/yFlODq6KwqMHy5Nv/55Xtnb27IgXJc8rCEhxnIbRak31cdVvXMn8fVLaTSIiKTMSgR+KFaYEXCZwb3/qWkdjsOylT0ihdYRHBUCgYEAx+f5EE0ne2r+5Hti63mTlnAhv+ZkHnZiZTDJQPhhpweoyZjriAgvBLvzPmijmZZkmQvpg2JdqC0RAkJtwogd54/6hupAercEJBk1megtVhOH6qjUGmKXAHiEkrNWVPQhNwfTPpLOOh6JPnou2gzs7opnliPRByPaXCL/Jhm2fz8CgYEAsGuVa2YMlMx5gjvyaHH2ZybStUQHPIR7k5lMHkZCKjyXVHCi9I8Iwvm+Thdr9qchWU8U5MYsTA8IkbHE5hbyEz3m8lpiJ2yUeb35vcNeCwJj6Me3TRNN8aMgg110tLdCF+jk/Q6MaftD1h19/XD9EWNgTjx/IuO7WVxDyaSz/XkCgYAumid342Sm4uSVAyamWmtLkMxtXhpM97AsgtkH6l9pfuGcTafqyG2dnusvy1kIPwUooJxJYq8Ou6LRcgcAaJcAGpJ+zTFG6k9u0ump/XREMr1muQDpPb6R/4Z4ZJJlr5vmpk5asgKdjezUwcsWThkV6vIHEEZ0cak//XCZwzjGJQKBgEV4w8dwmiCphUkNUYANYMzLEKA97w248z7x0kwmGYwpbP5lW8/Dcexbrhcyz/Mu+KMVFCCAFS/mtBCJQS5wfWxFxXpElto+4d9aTCa8FmRDQ1GfHtnqX2YByYvM+Qaf/NTmLiZamRK00KBUYWB2FLsqLqS4OlAajpRFyFlPiTth');
INSERT INTO "public"."component_config" VALUES ('7f1ea17b-7cfe-4cec-b860-cdf3e4bc9b96', '7dadd2a2-d750-4aa1-806b-ff0158cf0c1b', 'keyUse', 'ENC');
INSERT INTO "public"."component_config" VALUES ('367cecaa-19aa-4a49-8683-4cbda104dbe8', '44cbce4a-4909-4ef3-908e-7eb1e06ca3e1', 'kc.user.profile.config', '{"attributes":[{"name":"username","displayName":"${username}","validations":{"length":{"min":3,"max":255},"username-prohibited-characters":{},"up-username-not-idn-homograph":{}},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"email","displayName":"${email}","validations":{"email":{},"length":{"max":255}},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"firstName","displayName":"${firstName}","validations":{"length":{"max":255},"person-name-prohibited-characters":{}},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"lastName","displayName":"${lastName}","validations":{"length":{"max":255},"person-name-prohibited-characters":{}},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false}],"groups":[{"name":"user-metadata","displayHeader":"User metadata","displayDescription":"Attributes, which refer to user metadata"}]}');
INSERT INTO "public"."component_config" VALUES ('7b27e115-c4cf-4240-95f6-a443b5094ee2', 'f5a178cb-322a-4697-8834-b91582067aab', 'secret', 'CTdt7ygqNjBT5VUX-9zKwKjTIGkqhPAL-wEWy9NmMlXghc5LNTfPMH3o4ogkwu3EQltbEmJo5oogv-eXldzOP6Zc9CkXmPv4j1A2L6WmekN7jPCenUAJmg4t7k7-Osy57ZV-l15MdTr9JKenLkSHkT3al-ncyJR4T-R7PCNWr3k');
INSERT INTO "public"."component_config" VALUES ('685dce82-27eb-4852-9a0b-b61ac68ee754', 'f5a178cb-322a-4697-8834-b91582067aab', 'algorithm', 'HS512');
INSERT INTO "public"."component_config" VALUES ('2d5cf007-c7bc-49fc-9237-b92a78042474', 'f5a178cb-322a-4697-8834-b91582067aab', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('cad06780-fe17-4a9b-a2d0-5a4a8bf62b00', 'f5a178cb-322a-4697-8834-b91582067aab', 'kid', '18f4953d-76f5-4a75-948e-e67431ac2710');
INSERT INTO "public"."component_config" VALUES ('f6b48566-9fbc-4828-8393-be2c915a4310', '775f6650-1e03-472d-a3e0-004d863c2b40', 'keyUse', 'SIG');
INSERT INTO "public"."component_config" VALUES ('8631ef87-62ae-40de-b63b-d22f3c8a1dc5', '775f6650-1e03-472d-a3e0-004d863c2b40', 'certificate', 'MIICmTCCAYECBgGSuPfM/jANBgkqhkiG9w0BAQsFADAQMQ4wDAYDVQQDDAVzdXBvczAeFw0yNDEwMjMxMDQwMDBaFw0zNDEwMjMxMDQxNDBaMBAxDjAMBgNVBAMMBXN1cG9zMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAyK1UB9lpkP/ArlTwGgqTpKlshc8ZeGA8wINL3IzFNLaC01nAI3wfBkeG8x/+hY0I2T24uERfDA5YV4Ztoh1M0n7QSrr4ZIEhOyhyCy3O3DpZX/xszv7R9k21hPac6jmFh6mLfqna7yhH3FOh3JXjGM6PI33GAMEnvdhutanHaZVYeLfDUmR+fZ/H4+7WhYqej14F8OaoNAC45/59C3/HbuZzcUWYDqvF/hBgo/InHFcfW0pc26fPAWy7G8oAGhzBiJ3hOjapR60+LiHjX5Cowvih2xlGT5edt+eorZBB7O0gpdISgTt2K9ODB3p2ekn/PNlKyNdT84ouERkEC3Ca0QIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQCPZWOj07+GNzObSuVDxdkzSwQThvBMcEjl9RL+cZ6keor9NiAu7UCax+gVBPB86uKzCwh54qhG8lnuOZrcw4q17JKzo0gWehzbgvvW3o/jx6ufT/Y44E5dudcpItiIOFNeyhU7O0WkfFTIUx6Sz2V9fKoNLB67PwQRkEPkm+C2BJ5y1ohcT1vb8wEBo1mU9Ge6Caz1gxouhN/AvVx94Aly+5+2dclkmIWqG8zYC3kFbSc8S+6opmkBP85g/UY/Qlu6+ZmbQj+Dr+FdnN/hSdPNgzJja9ZA/urySsI4hZy/WDxS+BCbQZLpCMGtY9CPB7tuRvk5u9x2f/LZsAQU+6F+');
INSERT INTO "public"."component_config" VALUES ('935b5f16-8cb3-4aa7-95be-6a97086283ae', '775f6650-1e03-472d-a3e0-004d863c2b40', 'privateKey', 'MIIEowIBAAKCAQEAyK1UB9lpkP/ArlTwGgqTpKlshc8ZeGA8wINL3IzFNLaC01nAI3wfBkeG8x/+hY0I2T24uERfDA5YV4Ztoh1M0n7QSrr4ZIEhOyhyCy3O3DpZX/xszv7R9k21hPac6jmFh6mLfqna7yhH3FOh3JXjGM6PI33GAMEnvdhutanHaZVYeLfDUmR+fZ/H4+7WhYqej14F8OaoNAC45/59C3/HbuZzcUWYDqvF/hBgo/InHFcfW0pc26fPAWy7G8oAGhzBiJ3hOjapR60+LiHjX5Cowvih2xlGT5edt+eorZBB7O0gpdISgTt2K9ODB3p2ekn/PNlKyNdT84ouERkEC3Ca0QIDAQABAoIBAB0iQMlU8IULBEOq9CKrt2yX5jf07ZsyyY8fYKOX0keJWavqY0Ejk3N7beWdFvv3kjnLwqYJ0wyyhnPKgd8fxtfmbkHzK/2XW6Y1hH0H/PivBeuv/3H7v6q09G3lybkdebvMywIsEatX2zrq71xRbGNdFZ3O9DCG2kivA7+e8uLqeFn3fIO8/otG7f5GPMve4xDQ3u0MD4YQrR97mghTqSTkMd5ZIcS7VzthvuBmPrKf3qVoyMVnj6HahiVgDLKPuYht/DtlBMO+1pETgzjFN/riM7iISDFVvWixf7Bswqe0nsLpGrwR5rczWdOzhoNHi66DvSod59LtPofgOrkjk6MCgYEA5Ba+ppLwuR4yeYJPTbT5ZPiEnj9eKgcQjgpxo71dW+dCiSll/qFpUqGzILloHiVa+5oDwW82n3enpOy43YtiO6RTMuWdbczUU7irCZY8Y+dCUxGpmhuIcBfa6akTmBlbgE+NBvIBVdKdaWvL/PWkNVm2mEdxU6qAjYACshRi9WMCgYEA4TvdW+QSS5nazrt76JkmGWdjYzPM5nYCojDK0EskhP7JJYAYGXNdRvWcFRdEhR0tNaBwRETeExLW/ijD/h+YHurNdGeIqs+gVkLg1w+w1VejPekJn90ZlZs8bWLIDX0A/g0B6Pd8Paxxtc+uk5h52geHSBu+muuaiEDwlNgdzzsCgYAwz7FDIoUDiLPSjxF7lrQcaJaw6uyy38oqK5AAM4EsLsRtZ/+cy1wnw9T6ttLSSLo8x8vv9GXjII8u2z4Ao1iFXPg1FzBmlAQIWqe3qIAJ/S6Tal86TJQZMPG3OWipDxwmzF9o0hd5D1aCfgAshUD77dQGqJtXBVD3dyKci4JV8wKBgQCAnBxkEjFYNfw5O9kfEgQtUVnxFW0U06HhVxcYJTAvOQVGgoRAVB2ZHToI2QZpNCXSj1BLyz87iPB2pHR1sTi9vrmelFf3oSMe3oVgiDcjOy2ddmnmfOvU/5VbqKIvAYgFiQvkKR0qYkNz26kF4nUByHa4+A64i3vr/ZjihR1QbQKBgAF+CEa6iQvypBuCLGVII0bwB8mVyqq4PEk/JXeXXfDgf2WETap0MQWUHu3bpYGV17lMjPh89K7KEeN8L5X0PuJsOB788nRbzgxgW29cnxdySn7AO+QzT5sFXKciBr3UL86RUuX4eUKMNm4jHOdLloYYbzVNKN1IQoojeCp7D700');
INSERT INTO "public"."component_config" VALUES ('2aa88a19-803f-48b6-b16f-5b785bfede70', '775f6650-1e03-472d-a3e0-004d863c2b40', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('ae574647-1fee-4fcc-88fa-fd564f96f330', 'aeb804f9-595a-4394-b337-020f73bdc389', 'kid', '64497b7a-4566-4df9-b7c5-c48ff986adb2');
INSERT INTO "public"."component_config" VALUES ('b90ba69c-641b-451d-88d7-96afdd7a4411', 'aeb804f9-595a-4394-b337-020f73bdc389', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('d2abb8c1-7377-4a6c-8a40-a6fc048fbe72', 'aeb804f9-595a-4394-b337-020f73bdc389', 'secret', 'qyun8gttyJpCPwjqnp4XDA');
INSERT INTO "public"."component_config" VALUES ('d7fd5206-8f44-4ff2-ba81-ac53b008cdf9', '16d7a25b-a022-4cfb-8176-1059aeff83e8', 'algorithm', 'HS512');
INSERT INTO "public"."component_config" VALUES ('22f7c78a-d712-410f-80a0-babd7c9ff147', '16d7a25b-a022-4cfb-8176-1059aeff83e8', 'kid', '8ac93e41-b09a-4174-b5b0-94bca0f3201f');
INSERT INTO "public"."component_config" VALUES ('8714b0f8-eece-425f-848c-319426b78b07', '16d7a25b-a022-4cfb-8176-1059aeff83e8', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('6d6c272c-1cfb-4f5d-8f09-25fd04bc8156', '16d7a25b-a022-4cfb-8176-1059aeff83e8', 'secret', 'DMk9CGmAFy37p_fWacISzfTey2y5yQbWnryIQzAXB9CkODNyFZzCT2utytDPC61bGYATXuw1vktGyY1O9g5ZA8opPeVmT_hQA2I5N5UPcQIvDzdm5xdlXlhRlvH42dfm63NEjyBpIcrHzjrR0t_Wp_YODFfVBUynbwHgYsaQIUI');
INSERT INTO "public"."component_config" VALUES ('ba643d2b-6932-4123-8166-2867282a3331', '58f752c6-11ed-4224-b4ab-bd45d3c73654', 'keyUse', 'ENC');
INSERT INTO "public"."component_config" VALUES ('3b5d241c-4503-43a6-a4b6-4d3a005ee47f', '58f752c6-11ed-4224-b4ab-bd45d3c73654', 'priority', '100');
INSERT INTO "public"."component_config" VALUES ('112d33dc-5875-45c1-a5ac-62410c5881a1', '58f752c6-11ed-4224-b4ab-bd45d3c73654', 'algorithm', 'RSA-OAEP');
INSERT INTO "public"."component_config" VALUES ('98acc74d-a75e-4188-aef5-8893b7b86960', '58f752c6-11ed-4224-b4ab-bd45d3c73654', 'certificate', 'MIICmTCCAYECBgGSuPfNWzANBgkqhkiG9w0BAQsFADAQMQ4wDAYDVQQDDAVzdXBvczAeFw0yNDEwMjMxMDQwMDBaFw0zNDEwMjMxMDQxNDBaMBAxDjAMBgNVBAMMBXN1cG9zMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAoUA7REAxmlVWBn+Ca5wnLtadjaO8azcug5lx0YOiHlyMAdr9pKJH/+rzpGG0r3kxje2k954I1I1zEWZJYfs+7d3suBs8Z0+HvqTbG/2Msq1RNszCRr69NpEUF+5/RqUUS3/Cj4CTHhzNgyW41E/ehQuYK6r8Zz4CWLwf3LBUVJJCGnKMcoxuBefDeakOO6cJ5a9wHPR9dyHeyBqlD5NZOBHuv0252/FiElnFWsjaPr1x+fHOOi3NeP7wLrdS3sCHH+bOnK4nxNLW8F/5TbC82LSuc1wyJT6hFuBrPXoMoar1dj96VeXeRM0aXVo4GiWbh45qHVKSCqs9utucr0xcCwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQBp+Sq/5yhrn0mvb8TMtbzJ8U5A+G8upKQW5sgC+xq+sRgQ0XTFP5wndNI7nLl2ZMrpGem8AFsOBv7TrvXkJZKAJXt1c4yu18/tk+NUONU3k94uEP8d2WFKhTGwutswafxsx8XSidXik3warFeI6PDyZ7kwkMu8qLdVaKfvP4qGPpyleKas9hHi95TZt0fwpgnDW3pOCV9tS4zC4WsN3m5DfDDIrG3Qk8wJxQUHPrMCedpORWOBZRv3UCQwBbmc8gwIXdVFm2wyyBxu5+Aqyj0NaGJZ9tWKrhBS/JpKSTCr6xOgXqW/aD7qT3L15Yq+cWYeopemsA59Pgtn5VYBUuVr');
INSERT INTO "public"."component_config" VALUES ('c1e47b9b-5916-4d16-9671-dec97df52cc8', '58f752c6-11ed-4224-b4ab-bd45d3c73654', 'privateKey', 'MIIEowIBAAKCAQEAoUA7REAxmlVWBn+Ca5wnLtadjaO8azcug5lx0YOiHlyMAdr9pKJH/+rzpGG0r3kxje2k954I1I1zEWZJYfs+7d3suBs8Z0+HvqTbG/2Msq1RNszCRr69NpEUF+5/RqUUS3/Cj4CTHhzNgyW41E/ehQuYK6r8Zz4CWLwf3LBUVJJCGnKMcoxuBefDeakOO6cJ5a9wHPR9dyHeyBqlD5NZOBHuv0252/FiElnFWsjaPr1x+fHOOi3NeP7wLrdS3sCHH+bOnK4nxNLW8F/5TbC82LSuc1wyJT6hFuBrPXoMoar1dj96VeXeRM0aXVo4GiWbh45qHVKSCqs9utucr0xcCwIDAQABAoIBADAUGze33jJSlGI+nY/hUMuF4RcsxU7AdsV1OHsMQQfFd1dMSMlQO6CCGOAf48RYId7cBw5vl9lsTW1YLrQgufwpad3g/Qsequ48cDoxyMQzqh3pQlneoEMHUdLq4RcavGjgcI3h+7uEQgCC/E+Net73fIk0o6pS0ldLzEI8bwCB2+fVSU77yEtRyg3O50vLSQ5eUoueFI5qSxfbjyEttBbsj5H8bWERIkyq9TZZxjTWGS5nFCv5bzTFz+DMQmhVpVJE6sEumiRTJrK8XuAtltzBG/ruWcNk2b6aW1+ESBwOvEgvib3MztNX/gQyjbWVRJaOdFirVI3cmICWpE+Hx8ECgYEA2rUT22mPJuQobdLG0nfC34oyJxGvpEdac57gD2c8H5ke3vKWapOuHjcdiLsMPljCppH58AlJNh608R/PHB0xX/l1eL5MeEV3AIT7GU8mlMsrjLteBDU05LziFdTKzmiHo/n4G9RAYLiMInNCSlHh4RGCwByXjzu+6SeAMmgVWcsCgYEAvL8W7CStWFUEXYmTzZBHYZot40vBxvaU/iVL6tmpXxGEBIuY+495GUs5zTWFRrN9sWM1i+PMefpAgK9BQjfd9b2qFQLKh0+neCVvDz4NwxDeyG9WRJsQQG4JZCSEnghq2hIoEmRLA2fBvWwsHmKbkXvG1SzxrbH2IbcTN4XNvsECgYArS9mogBc0zcorI6T0mXzcoBEZpuisjuubJAKbSgafAsMXw9J/OskndiXEobLkzNGMBC4ElPIHYsDTU0a+/BCAPXRB+PpOfQH3+ltzQTYUEryGWblc/+N+vN3NEJktj4f6sEurxrMP8rjbQRIIgHlFAyBtQ7vFQUCkC4vXbr97TQKBgQCMElP0k5OBRjajJAJoP3Z53G3JjOMVwND5erxIYZfsUZdjuCWkKul39/fBbsKloXbaOgH2+us4apuL5IPNnKBASqz6QsQCfv6Nj1pIOYkFBnQO03F2II2DAyY9B0zT6vNBOtA6Nevlbw68gJaNRiilYvJAKcaBXNyIUNEWF1qFAQKBgER9btaMlEJagumXzNra7G0ksIbfCBJ8ZdQ2/kq8K5vBMyWPJzVv67riC6o+DSOceRpp2qbBCnVZ2YEFooNB5ZnnMWImrVw4gJsaTao57zNtkDK2gBRKf+u7XTS60YgKUoSLr4G1DGVGFpoUrIEhUgRWN/eNIe6WNt2UiaGxctiS');
INSERT INTO "public"."component_config" VALUES ('55508b00-f1e4-403a-9b31-cd7aac9d480e', '00d3022f-42ff-429c-add3-6083e760884e', 'allow-default-scopes', 'true');
INSERT INTO "public"."component_config" VALUES ('936e3529-124a-4286-aeb4-db57764eb9eb', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'saml-user-property-mapper');
INSERT INTO "public"."component_config" VALUES ('d916e288-0c8e-45cd-931b-51e5b41eaf18', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'oidc-full-name-mapper');
INSERT INTO "public"."component_config" VALUES ('3bba32cd-b1e4-4425-970d-5f1e0c5a65a2', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'oidc-address-mapper');
INSERT INTO "public"."component_config" VALUES ('ebd1f761-86c3-4036-b6f6-53eebca86722', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'oidc-sha256-pairwise-sub-mapper');
INSERT INTO "public"."component_config" VALUES ('a97f458d-bfa6-454d-908c-b10766f9cfb4', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'saml-role-list-mapper');
INSERT INTO "public"."component_config" VALUES ('cb09263e-a656-4bf5-ae19-90d36305e47c', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'saml-user-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('8a5278fd-f5db-4db1-8d94-2f5a706c018e', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'oidc-usermodel-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('3f97cb30-34c8-4235-94fc-6c460a5413ee', 'bbfb659a-6fa6-49ea-a818-926b58d6fada', 'allowed-protocol-mapper-types', 'oidc-usermodel-property-mapper');
INSERT INTO "public"."component_config" VALUES ('1d65a821-a9c1-416a-b4d3-828f2f5bd370', '3ee5ecda-1ea7-47b9-96ad-efc0280be683', 'client-uris-must-match', 'true');
INSERT INTO "public"."component_config" VALUES ('60e47f7d-0e94-4520-b678-1d213565dfbe', '3ee5ecda-1ea7-47b9-96ad-efc0280be683', 'host-sending-registration-request-must-match', 'true');
INSERT INTO "public"."component_config" VALUES ('9208771a-b1ca-4cb7-8fff-59eb98ffa75f', '988f193c-ecca-4580-ab7c-a79d701ae109', 'max-clients', '200');
INSERT INTO "public"."component_config" VALUES ('8a44a448-5ebc-41fe-8221-e7ddbf59e0d5', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'saml-user-property-mapper');
INSERT INTO "public"."component_config" VALUES ('343c1d05-7f69-4fce-add3-00a40ff29b71', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'oidc-full-name-mapper');
INSERT INTO "public"."component_config" VALUES ('408a13a5-1859-4c75-8145-3df8c970c531', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'saml-user-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('92b90b0e-e1fa-4416-9245-8266ceacbc31', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'oidc-usermodel-property-mapper');
INSERT INTO "public"."component_config" VALUES ('b72b980b-d7d2-40b5-bfdf-cef4feb18d58', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'saml-role-list-mapper');
INSERT INTO "public"."component_config" VALUES ('f4ef63ff-70bd-49cd-86d8-7c50301c6edb', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'oidc-address-mapper');
INSERT INTO "public"."component_config" VALUES ('6daef81a-3ec5-42c5-a437-67c74ec4ad45', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'oidc-sha256-pairwise-sub-mapper');
INSERT INTO "public"."component_config" VALUES ('9976f7e8-3d88-42ad-9fd8-e0988121f3ef', '42125d59-532f-4f8b-bf8e-d87f7c55c080', 'allowed-protocol-mapper-types', 'oidc-usermodel-attribute-mapper');
INSERT INTO "public"."component_config" VALUES ('5dcbea4c-6077-4ff0-9d5d-33a14fec3e00', '724be55b-51c3-45be-bf71-a5d7ae45b7ef', 'allow-default-scopes', 'true');
INSERT INTO "public"."component_config" VALUES ('8de327d0-8233-4cf8-9c6a-b766fcdc9b62', 'a2cf44c3-ba30-4e9d-93e2-973943bb60a3', 'kc.user.profile.config', '{"attributes":[{"name":"username","displayName":"${username}","validations":{"length":{"min":3,"max":255},"username-prohibited-characters":{},"up-username-not-idn-homograph":{}},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"email","displayName":"${email}","validations":{"email":{},"length":{"max":255}},"annotations":{},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"firstTimeLogin","displayName":"firstTimeLogin","validations":{},"annotations":{},"permissions":{"view":[],"edit":["admin"]},"multivalued":false},{"name":"firstName","displayName":"${firstName}","validations":{},"annotations":{},"permissions":{"view":[],"edit":["admin"]},"multivalued":false},{"name":"tipsEnable","displayName":"${profile.attributes.tipsEnable}","validations":{},"annotations":{},"permissions":{"view":["admin"],"edit":["admin"]},"multivalued":false},{"name":"homePage","displayName":"${profile.attributes.homePage}","validations":{},"annotations":{},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"phone","displayName":"${profile.attributes.phone}","validations":{},"annotations":{},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false},{"name":"source","displayName":"${profile.attributes.source}","validations":{},"annotations":{},"permissions":{"view":["admin","user"],"edit":["admin","user"]},"multivalued":false}],"groups":[{"name":"user-metadata","displayHeader":"User metadata","displayDescription":"Attributes, which refer to user metadata"}]}');

-- ----------------------------
-- Table structure for composite_role
-- ----------------------------
DROP TABLE IF EXISTS "public"."composite_role";
CREATE TABLE "public"."composite_role" (
  "composite" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "child_role" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of composite_role
-- ----------------------------
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '2fb3af6d-3c15-45b3-a116-74e376e1d09e');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '060b589b-7b85-4608-8862-a043fa0a03ad');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '9483ad2e-60d1-4b15-8266-cf6d559b0f7c');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '2d2f1b88-8ba1-4744-bc39-fea22ede194d');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'aadf1bdc-8e66-4a0b-a0e2-c4897de78357');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'df7ffc8b-2ac0-41ce-b0e3-3c6c4de4eedc');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'd8b3b296-0ad0-4f48-90e3-1c043bb6e1ec');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'bac80184-b8e1-4099-a08d-54d06499c34f');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '2aeb0a70-e557-4674-8e85-110a0a36af8f');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'b65d5f18-203c-4358-a0f1-ec664f30abdd');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '3f2ad7e1-a700-41df-8a97-70eae31b74f3');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '81ad7669-7a27-4e5f-8477-68b030404611');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'c34e146b-6303-4420-aba5-8716ec94f831');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'f07d7b23-ca05-4ce4-940d-e8fcf8f4341d');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '272ab104-177b-4a70-9d3c-3fe48a527e5b');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '4bb7a5f2-1770-4a13-b4bb-fb140f38c1f8');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '2e9ad6a3-001c-4980-a820-c9283691e44a');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '39516407-2312-480b-87e4-c21c1e85adf4');
INSERT INTO "public"."composite_role" VALUES ('2d2f1b88-8ba1-4744-bc39-fea22ede194d', '39516407-2312-480b-87e4-c21c1e85adf4');
INSERT INTO "public"."composite_role" VALUES ('2d2f1b88-8ba1-4744-bc39-fea22ede194d', '272ab104-177b-4a70-9d3c-3fe48a527e5b');
INSERT INTO "public"."composite_role" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', '80a9c87e-0298-43c6-9b83-a434aff9242e');
INSERT INTO "public"."composite_role" VALUES ('aadf1bdc-8e66-4a0b-a0e2-c4897de78357', '4bb7a5f2-1770-4a13-b4bb-fb140f38c1f8');
INSERT INTO "public"."composite_role" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', '6b07fca1-3f3a-438d-821b-4236995a1d27');
INSERT INTO "public"."composite_role" VALUES ('6b07fca1-3f3a-438d-821b-4236995a1d27', '020ebde6-eef1-4cd3-aff3-88981f134819');
INSERT INTO "public"."composite_role" VALUES ('542f5bc4-40ce-4913-98fb-79ba23afe6af', '654677e4-2e8c-4b64-9ec3-2c04f88cbcf2');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '78604774-c1b6-41a6-8807-f86fb24b21fa');
INSERT INTO "public"."composite_role" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', '80b2ebd4-bd1e-4449-b497-535ba5838c40');
INSERT INTO "public"."composite_role" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', '562f2fa0-5959-471d-b863-a648cfba36aa');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '1b77aefe-76bf-4abb-8497-dff7eaa27ae9');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '94e998e3-56eb-4041-8636-5403ef374959');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'ed36401b-28bd-4b41-928f-c9c408a91760');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'c80893f2-a0c1-44c3-92a4-13e1727ac3e3');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'a36edfae-7ae3-4b91-a3da-2ca591819d9c');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '864302b3-59bc-4849-9ae2-72d08b527be0');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '264402a2-05d4-4a20-9d6b-6463df42354b');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'd4d80f10-f129-4201-bc25-85a676f76af6');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '2ee8df51-85c0-4b5e-82bc-fe69f84bdba3');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'e715a939-c828-44db-a29f-d5dc830dd735');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '223fbdd1-640c-4e78-b059-fbe6ef340c1f');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'c3aec23b-c2c7-4daa-80a5-62f3d9c421d9');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'd5228208-c92b-4a39-9053-db2827876ad6');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'a8ac2545-ae16-444d-8630-47f9212f1c66');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'd7a520fa-2195-4da4-8c83-d26940a02be3');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'f2d71319-f501-4a02-ad64-d2515ca5964a');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '01e8a94c-36b3-41c5-869b-a4b8d9f46c2c');
INSERT INTO "public"."composite_role" VALUES ('c80893f2-a0c1-44c3-92a4-13e1727ac3e3', 'd7a520fa-2195-4da4-8c83-d26940a02be3');
INSERT INTO "public"."composite_role" VALUES ('ed36401b-28bd-4b41-928f-c9c408a91760', 'a8ac2545-ae16-444d-8630-47f9212f1c66');
INSERT INTO "public"."composite_role" VALUES ('ed36401b-28bd-4b41-928f-c9c408a91760', '01e8a94c-36b3-41c5-869b-a4b8d9f46c2c');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'a54b0c3e-63be-4650-9645-24f079ccad67');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'dc731ee5-13ea-4530-966a-5657179d3054');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'edac0850-db9a-4eac-8b34-3b046ecfea41');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '801d0c19-5995-4bdf-bd18-7c3e319af993');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'e211771c-a307-4607-b284-66fe1fab0bd1');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '76aaa660-3611-46a3-8c8e-1d0c81c345a2');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '7375196b-bd3e-4352-a70b-942cc885b0e9');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'ea50dad8-316b-4e11-830e-c59f57bd1ba9');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '52e39f80-cd4e-4469-8683-383cd80b6f69');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '36e554a4-010a-4e99-b10f-72a1fe910f18');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'a3cef62a-a518-4772-a27f-bab74d1aa5e4');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '44312bf7-797b-4ad1-86c6-01126d16fb8f');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'bc870077-52cf-4c18-b309-646353e532f3');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'eb9eb0bd-b100-40a2-bba3-fee42a156d0b');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', 'bba48ce9-71db-4013-a524-121b4410b4a9');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '045f3abc-30ce-4419-b1b8-d648764bf324');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '5ade70e4-7643-497e-b0bf-0746538c609a');
INSERT INTO "public"."composite_role" VALUES ('801d0c19-5995-4bdf-bd18-7c3e319af993', 'bba48ce9-71db-4013-a524-121b4410b4a9');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '32efd528-0e19-4c16-b654-fd3f80a824e7');
INSERT INTO "public"."composite_role" VALUES ('edac0850-db9a-4eac-8b34-3b046ecfea41', 'eb9eb0bd-b100-40a2-bba3-fee42a156d0b');
INSERT INTO "public"."composite_role" VALUES ('edac0850-db9a-4eac-8b34-3b046ecfea41', '5ade70e4-7643-497e-b0bf-0746538c609a');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', 'f9190462-32f2-4183-a192-20c1525b9b1a');
INSERT INTO "public"."composite_role" VALUES ('f9190462-32f2-4183-a192-20c1525b9b1a', '7b245f39-d8e4-4186-bc14-5b4561139968');
INSERT INTO "public"."composite_role" VALUES ('020a4c2e-a65d-4eb9-9c92-7a238913654c', 'eff80418-df2b-407f-8589-6d4856f10fd1');
INSERT INTO "public"."composite_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '75e93375-d75f-454d-b9f3-7a9c46fc02d2');
INSERT INTO "public"."composite_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '09e0c927-c268-4f7e-af09-a9c46a413910');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', 'c4cc1aab-5a86-4697-a33e-4976807b8563');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '75586479-0c1a-458b-a3d8-6486ad42e7ad');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '09e0c927-c268-4f7e-af09-a9c46a413910');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '954a1660-6dc1-4ae5-b6f7-d2706bed7df2');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', 'f9190462-32f2-4183-a192-20c1525b9b1a');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '020a4c2e-a65d-4eb9-9c92-7a238913654c');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '32efd528-0e19-4c16-b654-fd3f80a824e7');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '7b245f39-d8e4-4186-bc14-5b4561139968');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '7d347427-5196-476b-b265-caa86c3d6ff9');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', 'ed0e5ab3-3b89-4625-86be-e2f405638793');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', '18ea01e0-8942-4c4c-9c06-6cfb0946517a');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', 'a54b0c3e-63be-4650-9645-24f079ccad67');
INSERT INTO "public"."composite_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', 'eff80418-df2b-407f-8589-6d4856f10fd1');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '09e0c927-c268-4f7e-af09-a9c46a413910');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '954a1660-6dc1-4ae5-b6f7-d2706bed7df2');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '020a4c2e-a65d-4eb9-9c92-7a238913654c');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '7b245f39-d8e4-4186-bc14-5b4561139968');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '7d347427-5196-476b-b265-caa86c3d6ff9');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', 'ed0e5ab3-3b89-4625-86be-e2f405638793');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '18ea01e0-8942-4c4c-9c06-6cfb0946517a');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', 'a54b0c3e-63be-4650-9645-24f079ccad67');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', 'eff80418-df2b-407f-8589-6d4856f10fd1');
INSERT INTO "public"."composite_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '2152d19d-e4f9-488d-8509-e49cf239596a');

-- ----------------------------
-- Table structure for credential
-- ----------------------------
DROP TABLE IF EXISTS "public"."credential";
CREATE TABLE "public"."credential" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "salt" bytea,
  "type" varchar(255) COLLATE "pg_catalog"."default",
  "user_id" varchar(36) COLLATE "pg_catalog"."default",
  "created_date" int8,
  "user_label" varchar(255) COLLATE "pg_catalog"."default",
  "secret_data" text COLLATE "pg_catalog"."default",
  "credential_data" text COLLATE "pg_catalog"."default",
  "priority" int4
)
;

-- ----------------------------
-- Records of credential
-- ----------------------------
INSERT INTO "public"."credential" VALUES ('d3c688d7-ec6c-4ccb-be70-9e0634124421', NULL, 'password', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22', 1750217451325, '我的密码', '{"value":"xKCL2W4PCp2mPpSDRq4EUeNKr8V4jDugHSPdfHrbla4=","salt":"fxlzXa4K1NSKlRbONiHZ9A==","additionalParameters":{}}', '{"hashIterations":5,"algorithm":"argon2","additionalParameters":{"hashLength":["32"],"memory":["7168"],"type":["id"],"version":["1.3"],"parallelism":["1"]}}', 10);
INSERT INTO "public"."credential" VALUES ('588747a6-3f05-497a-bc78-81b7fe3d0e93', NULL, 'password', '66b5114b-0083-48aa-860a-06f1c06ce4c4', 1746512526549, 'My password', '{"value":"75zzlRwO66cfDFCPnpNS3XAZ+HF7nTR+cbj/jVSsAcA=","salt":"y5WNIOmeIfBYdGecbhKQ0Q==","additionalParameters":{}}', '{"hashIterations":5,"algorithm":"argon2","additionalParameters":{"hashLength":["32"],"memory":["7168"],"type":["id"],"version":["1.3"],"parallelism":["1"]}}', 10);

-- ----------------------------
-- Table structure for databasechangelog
-- ----------------------------
DROP TABLE IF EXISTS "public"."databasechangelog";
CREATE TABLE "public"."databasechangelog" (
  "id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "author" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "filename" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "dateexecuted" timestamp(6) NOT NULL,
  "orderexecuted" int4 NOT NULL,
  "exectype" varchar(10) COLLATE "pg_catalog"."default" NOT NULL,
  "md5sum" varchar(35) COLLATE "pg_catalog"."default",
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "comments" varchar(255) COLLATE "pg_catalog"."default",
  "tag" varchar(255) COLLATE "pg_catalog"."default",
  "liquibase" varchar(20) COLLATE "pg_catalog"."default",
  "contexts" varchar(255) COLLATE "pg_catalog"."default",
  "labels" varchar(255) COLLATE "pg_catalog"."default",
  "deployment_id" varchar(10) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of databasechangelog
-- ----------------------------
INSERT INTO "public"."databasechangelog" VALUES ('1.0.0.Final-KEYCLOAK-5461', 'sthorger@redhat.com', 'META-INF/jpa-changelog-1.0.0.Final.xml', '2024-10-23 10:39:03.719037', 1, 'EXECUTED', '9:6f1016664e21e16d26517a4418f5e3df', 'createTable tableName=APPLICATION_DEFAULT_ROLES; createTable tableName=CLIENT; createTable tableName=CLIENT_SESSION; createTable tableName=CLIENT_SESSION_ROLE; createTable tableName=COMPOSITE_ROLE; createTable tableName=CREDENTIAL; createTable tab...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.0.0.Final-KEYCLOAK-5461', 'sthorger@redhat.com', 'META-INF/db2-jpa-changelog-1.0.0.Final.xml', '2024-10-23 10:39:03.74104', 2, 'MARK_RAN', '9:828775b1596a07d1200ba1d49e5e3941', 'createTable tableName=APPLICATION_DEFAULT_ROLES; createTable tableName=CLIENT; createTable tableName=CLIENT_SESSION; createTable tableName=CLIENT_SESSION_ROLE; createTable tableName=COMPOSITE_ROLE; createTable tableName=CREDENTIAL; createTable tab...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.1.0.Beta1', 'sthorger@redhat.com', 'META-INF/jpa-changelog-1.1.0.Beta1.xml', '2024-10-23 10:39:03.788967', 3, 'EXECUTED', '9:5f090e44a7d595883c1fb61f4b41fd38', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION; createTable tableName=CLIENT_ATTRIBUTES; createTable tableName=CLIENT_SESSION_NOTE; createTable tableName=APP_NODE_REGISTRATIONS; addColumn table...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.1.0.Final', 'sthorger@redhat.com', 'META-INF/jpa-changelog-1.1.0.Final.xml', '2024-10-23 10:39:03.795029', 4, 'EXECUTED', '9:c07e577387a3d2c04d1adc9aaad8730e', 'renameColumn newColumnName=EVENT_TIME, oldColumnName=TIME, tableName=EVENT_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.2.0.Beta1', 'psilva@redhat.com', 'META-INF/jpa-changelog-1.2.0.Beta1.xml', '2024-10-23 10:39:03.877685', 5, 'EXECUTED', '9:b68ce996c655922dbcd2fe6b6ae72686', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION; createTable tableName=PROTOCOL_MAPPER; createTable tableName=PROTOCOL_MAPPER_CONFIG; createTable tableName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.2.0.Beta1', 'psilva@redhat.com', 'META-INF/db2-jpa-changelog-1.2.0.Beta1.xml', '2024-10-23 10:39:03.884766', 6, 'MARK_RAN', '9:543b5c9989f024fe35c6f6c5a97de88e', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION; createTable tableName=PROTOCOL_MAPPER; createTable tableName=PROTOCOL_MAPPER_CONFIG; createTable tableName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.2.0.RC1', 'bburke@redhat.com', 'META-INF/jpa-changelog-1.2.0.CR1.xml', '2024-10-23 10:39:03.956856', 7, 'EXECUTED', '9:765afebbe21cf5bbca048e632df38336', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete tableName=USER_SESSION; createTable tableName=MIGRATION_MODEL; createTable tableName=IDENTITY_P...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.2.0.RC1', 'bburke@redhat.com', 'META-INF/db2-jpa-changelog-1.2.0.CR1.xml', '2024-10-23 10:39:03.966292', 8, 'MARK_RAN', '9:db4a145ba11a6fdaefb397f6dbf829a1', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete tableName=USER_SESSION; createTable tableName=MIGRATION_MODEL; createTable tableName=IDENTITY_P...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.2.0.Final', 'keycloak', 'META-INF/jpa-changelog-1.2.0.Final.xml', '2024-10-23 10:39:03.975365', 9, 'EXECUTED', '9:9d05c7be10cdb873f8bcb41bc3a8ab23', 'update tableName=CLIENT; update tableName=CLIENT; update tableName=CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.3.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-1.3.0.xml', '2024-10-23 10:39:04.044289', 10, 'EXECUTED', '9:18593702353128d53111f9b1ff0b82b8', 'delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_PROT_MAPPER; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete tableName=USER_SESSION; createTable tableName=ADMI...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.4.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-1.4.0.xml', '2024-10-23 10:39:04.091754', 11, 'EXECUTED', '9:6122efe5f090e41a85c0f1c9e52cbb62', 'delete tableName=CLIENT_SESSION_AUTH_STATUS; delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_PROT_MAPPER; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete table...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.4.0', 'bburke@redhat.com', 'META-INF/db2-jpa-changelog-1.4.0.xml', '2024-10-23 10:39:04.099685', 12, 'MARK_RAN', '9:e1ff28bf7568451453f844c5d54bb0b5', 'delete tableName=CLIENT_SESSION_AUTH_STATUS; delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_PROT_MAPPER; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete table...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.5.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-1.5.0.xml', '2024-10-23 10:39:04.124833', 13, 'EXECUTED', '9:7af32cd8957fbc069f796b61217483fd', 'delete tableName=CLIENT_SESSION_AUTH_STATUS; delete tableName=CLIENT_SESSION_ROLE; delete tableName=CLIENT_SESSION_PROT_MAPPER; delete tableName=CLIENT_SESSION_NOTE; delete tableName=CLIENT_SESSION; delete tableName=USER_SESSION_NOTE; delete table...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.6.1_from15', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.6.1.xml', '2024-10-23 10:39:04.139029', 14, 'EXECUTED', '9:6005e15e84714cd83226bf7879f54190', 'addColumn tableName=REALM; addColumn tableName=KEYCLOAK_ROLE; addColumn tableName=CLIENT; createTable tableName=OFFLINE_USER_SESSION; createTable tableName=OFFLINE_CLIENT_SESSION; addPrimaryKey constraintName=CONSTRAINT_OFFL_US_SES_PK2, tableName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.6.1_from16-pre', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.6.1.xml', '2024-10-23 10:39:04.141223', 15, 'MARK_RAN', '9:bf656f5a2b055d07f314431cae76f06c', 'delete tableName=OFFLINE_CLIENT_SESSION; delete tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.6.1_from16', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.6.1.xml', '2024-10-23 10:39:04.14425', 16, 'MARK_RAN', '9:f8dadc9284440469dcf71e25ca6ab99b', 'dropPrimaryKey constraintName=CONSTRAINT_OFFLINE_US_SES_PK, tableName=OFFLINE_USER_SESSION; dropPrimaryKey constraintName=CONSTRAINT_OFFLINE_CL_SES_PK, tableName=OFFLINE_CLIENT_SESSION; addColumn tableName=OFFLINE_USER_SESSION; update tableName=OF...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.6.1', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.6.1.xml', '2024-10-23 10:39:04.147857', 17, 'EXECUTED', '9:d41d8cd98f00b204e9800998ecf8427e', 'empty', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.7.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-1.7.0.xml', '2024-10-23 10:39:04.186832', 18, 'EXECUTED', '9:3368ff0be4c2855ee2dd9ca813b38d8e', 'createTable tableName=KEYCLOAK_GROUP; createTable tableName=GROUP_ROLE_MAPPING; createTable tableName=GROUP_ATTRIBUTE; createTable tableName=USER_GROUP_MEMBERSHIP; createTable tableName=REALM_DEFAULT_GROUPS; addColumn tableName=IDENTITY_PROVIDER; ...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.8.0', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.8.0.xml', '2024-10-23 10:39:04.224289', 19, 'EXECUTED', '9:8ac2fb5dd030b24c0570a763ed75ed20', 'addColumn tableName=IDENTITY_PROVIDER; createTable tableName=CLIENT_TEMPLATE; createTable tableName=CLIENT_TEMPLATE_ATTRIBUTES; createTable tableName=TEMPLATE_SCOPE_MAPPING; dropNotNullConstraint columnName=CLIENT_ID, tableName=PROTOCOL_MAPPER; ad...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.8.0-2', 'keycloak', 'META-INF/jpa-changelog-1.8.0.xml', '2024-10-23 10:39:04.23023', 20, 'EXECUTED', '9:f91ddca9b19743db60e3057679810e6c', 'dropDefaultValue columnName=ALGORITHM, tableName=CREDENTIAL; update tableName=CREDENTIAL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-33201-org-redirect-url', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.923028', 144, 'EXECUTED', '9:4d0e22b0ac68ebe9794fa9cb752ea660', 'addColumn tableName=ORG', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.8.0', 'mposolda@redhat.com', 'META-INF/db2-jpa-changelog-1.8.0.xml', '2024-10-23 10:39:04.235233', 21, 'MARK_RAN', '9:831e82914316dc8a57dc09d755f23c51', 'addColumn tableName=IDENTITY_PROVIDER; createTable tableName=CLIENT_TEMPLATE; createTable tableName=CLIENT_TEMPLATE_ATTRIBUTES; createTable tableName=TEMPLATE_SCOPE_MAPPING; dropNotNullConstraint columnName=CLIENT_ID, tableName=PROTOCOL_MAPPER; ad...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.8.0-2', 'keycloak', 'META-INF/db2-jpa-changelog-1.8.0.xml', '2024-10-23 10:39:04.23819', 22, 'MARK_RAN', '9:f91ddca9b19743db60e3057679810e6c', 'dropDefaultValue columnName=ALGORITHM, tableName=CREDENTIAL; update tableName=CREDENTIAL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.9.0', 'mposolda@redhat.com', 'META-INF/jpa-changelog-1.9.0.xml', '2024-10-23 10:39:04.318306', 23, 'EXECUTED', '9:bc3d0f9e823a69dc21e23e94c7a94bb1', 'update tableName=REALM; update tableName=REALM; update tableName=REALM; update tableName=REALM; update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=REALM; update tableName=REALM; customChange; dr...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.9.1', 'keycloak', 'META-INF/jpa-changelog-1.9.1.xml', '2024-10-23 10:39:04.32419', 24, 'EXECUTED', '9:c9999da42f543575ab790e76439a2679', 'modifyDataType columnName=PRIVATE_KEY, tableName=REALM; modifyDataType columnName=PUBLIC_KEY, tableName=REALM; modifyDataType columnName=CERTIFICATE, tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.9.1', 'keycloak', 'META-INF/db2-jpa-changelog-1.9.1.xml', '2024-10-23 10:39:04.325884', 25, 'MARK_RAN', '9:0d6c65c6f58732d81569e77b10ba301d', 'modifyDataType columnName=PRIVATE_KEY, tableName=REALM; modifyDataType columnName=CERTIFICATE, tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('1.9.2', 'keycloak', 'META-INF/jpa-changelog-1.9.2.xml', '2024-10-23 10:39:04.742144', 26, 'EXECUTED', '9:fc576660fc016ae53d2d4778d84d86d0', 'createIndex indexName=IDX_USER_EMAIL, tableName=USER_ENTITY; createIndex indexName=IDX_USER_ROLE_MAPPING, tableName=USER_ROLE_MAPPING; createIndex indexName=IDX_USER_GROUP_MAPPING, tableName=USER_GROUP_MEMBERSHIP; createIndex indexName=IDX_USER_CO...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-2.0.0', 'psilva@redhat.com', 'META-INF/jpa-changelog-authz-2.0.0.xml', '2024-10-23 10:39:04.782081', 27, 'EXECUTED', '9:43ed6b0da89ff77206289e87eaa9c024', 'createTable tableName=RESOURCE_SERVER; addPrimaryKey constraintName=CONSTRAINT_FARS, tableName=RESOURCE_SERVER; addUniqueConstraint constraintName=UK_AU8TT6T700S9V50BU18WS5HA6, tableName=RESOURCE_SERVER; createTable tableName=RESOURCE_SERVER_RESOU...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-2.5.1', 'psilva@redhat.com', 'META-INF/jpa-changelog-authz-2.5.1.xml', '2024-10-23 10:39:04.78589', 28, 'EXECUTED', '9:44bae577f551b3738740281eceb4ea70', 'update tableName=RESOURCE_SERVER_POLICY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.1.0-KEYCLOAK-5461', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.1.0.xml', '2024-10-23 10:39:04.818902', 29, 'EXECUTED', '9:bd88e1f833df0420b01e114533aee5e8', 'createTable tableName=BROKER_LINK; createTable tableName=FED_USER_ATTRIBUTE; createTable tableName=FED_USER_CONSENT; createTable tableName=FED_USER_CONSENT_ROLE; createTable tableName=FED_USER_CONSENT_PROT_MAPPER; createTable tableName=FED_USER_CR...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.2.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.2.0.xml', '2024-10-23 10:39:04.829735', 30, 'EXECUTED', '9:a7022af5267f019d020edfe316ef4371', 'addColumn tableName=ADMIN_EVENT_ENTITY; createTable tableName=CREDENTIAL_ATTRIBUTE; createTable tableName=FED_CREDENTIAL_ATTRIBUTE; modifyDataType columnName=VALUE, tableName=CREDENTIAL; addForeignKeyConstraint baseTableName=FED_CREDENTIAL_ATTRIBU...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.3.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.3.0.xml', '2024-10-23 10:39:04.844727', 31, 'EXECUTED', '9:fc155c394040654d6a79227e56f5e25a', 'createTable tableName=FEDERATED_USER; addPrimaryKey constraintName=CONSTR_FEDERATED_USER, tableName=FEDERATED_USER; dropDefaultValue columnName=TOTP, tableName=USER_ENTITY; dropColumn columnName=TOTP, tableName=USER_ENTITY; addColumn tableName=IDE...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.4.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.4.0.xml', '2024-10-23 10:39:04.849557', 32, 'EXECUTED', '9:eac4ffb2a14795e5dc7b426063e54d88', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.5.0.xml', '2024-10-23 10:39:04.855921', 33, 'EXECUTED', '9:54937c05672568c4c64fc9524c1e9462', 'customChange; modifyDataType columnName=USER_ID, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.0-unicode-oracle', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-2.5.0.xml', '2024-10-23 10:39:04.858229', 34, 'MARK_RAN', '9:3a32bace77c84d7678d035a7f5a8084e', 'modifyDataType columnName=DESCRIPTION, tableName=AUTHENTICATION_FLOW; modifyDataType columnName=DESCRIPTION, tableName=CLIENT_TEMPLATE; modifyDataType columnName=DESCRIPTION, tableName=RESOURCE_SERVER_POLICY; modifyDataType columnName=DESCRIPTION,...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.0-unicode-other-dbs', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-2.5.0.xml', '2024-10-23 10:39:04.878783', 35, 'EXECUTED', '9:33d72168746f81f98ae3a1e8e0ca3554', 'modifyDataType columnName=DESCRIPTION, tableName=AUTHENTICATION_FLOW; modifyDataType columnName=DESCRIPTION, tableName=CLIENT_TEMPLATE; modifyDataType columnName=DESCRIPTION, tableName=RESOURCE_SERVER_POLICY; modifyDataType columnName=DESCRIPTION,...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.0-duplicate-email-support', 'slawomir@dabek.name', 'META-INF/jpa-changelog-2.5.0.xml', '2024-10-23 10:39:04.884587', 36, 'EXECUTED', '9:61b6d3d7a4c0e0024b0c839da283da0c', 'addColumn tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.0-unique-group-names', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-2.5.0.xml', '2024-10-23 10:39:04.88866', 37, 'EXECUTED', '9:8dcac7bdf7378e7d823cdfddebf72fda', 'addUniqueConstraint constraintName=SIBLING_NAMES, tableName=KEYCLOAK_GROUP', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('2.5.1', 'bburke@redhat.com', 'META-INF/jpa-changelog-2.5.1.xml', '2024-10-23 10:39:04.892762', 38, 'EXECUTED', '9:a2b870802540cb3faa72098db5388af3', 'addColumn tableName=FED_USER_CONSENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.0.0', 'bburke@redhat.com', 'META-INF/jpa-changelog-3.0.0.xml', '2024-10-23 10:39:04.896385', 39, 'EXECUTED', '9:132a67499ba24bcc54fb5cbdcfe7e4c0', 'addColumn tableName=IDENTITY_PROVIDER', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.2.0-fix', 'keycloak', 'META-INF/jpa-changelog-3.2.0.xml', '2024-10-23 10:39:04.89785', 40, 'MARK_RAN', '9:938f894c032f5430f2b0fafb1a243462', 'addNotNullConstraint columnName=REALM_ID, tableName=CLIENT_INITIAL_ACCESS', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.2.0-fix-with-keycloak-5416', 'keycloak', 'META-INF/jpa-changelog-3.2.0.xml', '2024-10-23 10:39:04.900024', 41, 'MARK_RAN', '9:845c332ff1874dc5d35974b0babf3006', 'dropIndex indexName=IDX_CLIENT_INIT_ACC_REALM, tableName=CLIENT_INITIAL_ACCESS; addNotNullConstraint columnName=REALM_ID, tableName=CLIENT_INITIAL_ACCESS; createIndex indexName=IDX_CLIENT_INIT_ACC_REALM, tableName=CLIENT_INITIAL_ACCESS', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.2.0-fix-offline-sessions', 'hmlnarik', 'META-INF/jpa-changelog-3.2.0.xml', '2024-10-23 10:39:04.905247', 42, 'EXECUTED', '9:fc86359c079781adc577c5a217e4d04c', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.2.0-fixed', 'keycloak', 'META-INF/jpa-changelog-3.2.0.xml', '2024-10-23 10:39:06.413742', 43, 'EXECUTED', '9:59a64800e3c0d09b825f8a3b444fa8f4', 'addColumn tableName=REALM; dropPrimaryKey constraintName=CONSTRAINT_OFFL_CL_SES_PK2, tableName=OFFLINE_CLIENT_SESSION; dropColumn columnName=CLIENT_SESSION_ID, tableName=OFFLINE_CLIENT_SESSION; addPrimaryKey constraintName=CONSTRAINT_OFFL_CL_SES_P...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.3.0', 'keycloak', 'META-INF/jpa-changelog-3.3.0.xml', '2024-10-23 10:39:06.417938', 44, 'EXECUTED', '9:d48d6da5c6ccf667807f633fe489ce88', 'addColumn tableName=USER_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-3.4.0.CR1-resource-server-pk-change-part1', 'glavoie@gmail.com', 'META-INF/jpa-changelog-authz-3.4.0.CR1.xml', '2024-10-23 10:39:06.422232', 45, 'EXECUTED', '9:dde36f7973e80d71fceee683bc5d2951', 'addColumn tableName=RESOURCE_SERVER_POLICY; addColumn tableName=RESOURCE_SERVER_RESOURCE; addColumn tableName=RESOURCE_SERVER_SCOPE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-3.4.0.CR1-resource-server-pk-change-part2-KEYCLOAK-6095', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-authz-3.4.0.CR1.xml', '2024-10-23 10:39:06.426355', 46, 'EXECUTED', '9:b855e9b0a406b34fa323235a0cf4f640', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-3.4.0.CR1-resource-server-pk-change-part3-fixed', 'glavoie@gmail.com', 'META-INF/jpa-changelog-authz-3.4.0.CR1.xml', '2024-10-23 10:39:06.427904', 47, 'MARK_RAN', '9:51abbacd7b416c50c4421a8cabf7927e', 'dropIndex indexName=IDX_RES_SERV_POL_RES_SERV, tableName=RESOURCE_SERVER_POLICY; dropIndex indexName=IDX_RES_SRV_RES_RES_SRV, tableName=RESOURCE_SERVER_RESOURCE; dropIndex indexName=IDX_RES_SRV_SCOPE_RES_SRV, tableName=RESOURCE_SERVER_SCOPE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-3.4.0.CR1-resource-server-pk-change-part3-fixed-nodropindex', 'glavoie@gmail.com', 'META-INF/jpa-changelog-authz-3.4.0.CR1.xml', '2024-10-23 10:39:06.543204', 48, 'EXECUTED', '9:bdc99e567b3398bac83263d375aad143', 'addNotNullConstraint columnName=RESOURCE_SERVER_CLIENT_ID, tableName=RESOURCE_SERVER_POLICY; addNotNullConstraint columnName=RESOURCE_SERVER_CLIENT_ID, tableName=RESOURCE_SERVER_RESOURCE; addNotNullConstraint columnName=RESOURCE_SERVER_CLIENT_ID, ...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authn-3.4.0.CR1-refresh-token-max-reuse', 'glavoie@gmail.com', 'META-INF/jpa-changelog-authz-3.4.0.CR1.xml', '2024-10-23 10:39:06.548814', 49, 'EXECUTED', '9:d198654156881c46bfba39abd7769e69', 'addColumn tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.4.0', 'keycloak', 'META-INF/jpa-changelog-3.4.0.xml', '2024-10-23 10:39:06.566722', 50, 'EXECUTED', '9:cfdd8736332ccdd72c5256ccb42335db', 'addPrimaryKey constraintName=CONSTRAINT_REALM_DEFAULT_ROLES, tableName=REALM_DEFAULT_ROLES; addPrimaryKey constraintName=CONSTRAINT_COMPOSITE_ROLE, tableName=COMPOSITE_ROLE; addPrimaryKey constraintName=CONSTR_REALM_DEFAULT_GROUPS, tableName=REALM...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.4.0-KEYCLOAK-5230', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-3.4.0.xml', '2024-10-23 10:39:06.927074', 51, 'EXECUTED', '9:7c84de3d9bd84d7f077607c1a4dcb714', 'createIndex indexName=IDX_FU_ATTRIBUTE, tableName=FED_USER_ATTRIBUTE; createIndex indexName=IDX_FU_CONSENT, tableName=FED_USER_CONSENT; createIndex indexName=IDX_FU_CONSENT_RU, tableName=FED_USER_CONSENT; createIndex indexName=IDX_FU_CREDENTIAL, t...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.4.1', 'psilva@redhat.com', 'META-INF/jpa-changelog-3.4.1.xml', '2024-10-23 10:39:06.930545', 52, 'EXECUTED', '9:5a6bb36cbefb6a9d6928452c0852af2d', 'modifyDataType columnName=VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.4.2', 'keycloak', 'META-INF/jpa-changelog-3.4.2.xml', '2024-10-23 10:39:06.933388', 53, 'EXECUTED', '9:8f23e334dbc59f82e0a328373ca6ced0', 'update tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('3.4.2-KEYCLOAK-5172', 'mkanis@redhat.com', 'META-INF/jpa-changelog-3.4.2.xml', '2024-10-23 10:39:06.935958', 54, 'EXECUTED', '9:9156214268f09d970cdf0e1564d866af', 'update tableName=CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.0.0-KEYCLOAK-6335', 'bburke@redhat.com', 'META-INF/jpa-changelog-4.0.0.xml', '2024-10-23 10:39:06.941921', 55, 'EXECUTED', '9:db806613b1ed154826c02610b7dbdf74', 'createTable tableName=CLIENT_AUTH_FLOW_BINDINGS; addPrimaryKey constraintName=C_CLI_FLOW_BIND, tableName=CLIENT_AUTH_FLOW_BINDINGS', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.0.0-CLEANUP-UNUSED-TABLE', 'bburke@redhat.com', 'META-INF/jpa-changelog-4.0.0.xml', '2024-10-23 10:39:06.947413', 56, 'EXECUTED', '9:229a041fb72d5beac76bb94a5fa709de', 'dropTable tableName=CLIENT_IDENTITY_PROV_MAPPING', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.0.0-KEYCLOAK-6228', 'bburke@redhat.com', 'META-INF/jpa-changelog-4.0.0.xml', '2024-10-23 10:39:06.992362', 57, 'EXECUTED', '9:079899dade9c1e683f26b2aa9ca6ff04', 'dropUniqueConstraint constraintName=UK_JKUWUVD56ONTGSUHOGM8UEWRT, tableName=USER_CONSENT; dropNotNullConstraint columnName=CLIENT_ID, tableName=USER_CONSENT; addColumn tableName=USER_CONSENT; addUniqueConstraint constraintName=UK_JKUWUVD56ONTGSUHO...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.0.0-KEYCLOAK-5579-fixed', 'mposolda@redhat.com', 'META-INF/jpa-changelog-4.0.0.xml', '2024-10-23 10:39:07.352722', 58, 'EXECUTED', '9:139b79bcbbfe903bb1c2d2a4dbf001d9', 'dropForeignKeyConstraint baseTableName=CLIENT_TEMPLATE_ATTRIBUTES, constraintName=FK_CL_TEMPL_ATTR_TEMPL; renameTable newTableName=CLIENT_SCOPE_ATTRIBUTES, oldTableName=CLIENT_TEMPLATE_ATTRIBUTES; renameColumn newColumnName=SCOPE_ID, oldColumnName...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-4.0.0.CR1', 'psilva@redhat.com', 'META-INF/jpa-changelog-authz-4.0.0.CR1.xml', '2024-10-23 10:39:07.369943', 59, 'EXECUTED', '9:b55738ad889860c625ba2bf483495a04', 'createTable tableName=RESOURCE_SERVER_PERM_TICKET; addPrimaryKey constraintName=CONSTRAINT_FAPMT, tableName=RESOURCE_SERVER_PERM_TICKET; addForeignKeyConstraint baseTableName=RESOURCE_SERVER_PERM_TICKET, constraintName=FK_FRSRHO213XCX4WNKOG82SSPMT...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-4.0.0.Beta3', 'psilva@redhat.com', 'META-INF/jpa-changelog-authz-4.0.0.Beta3.xml', '2024-10-23 10:39:07.375282', 60, 'EXECUTED', '9:e0057eac39aa8fc8e09ac6cfa4ae15fe', 'addColumn tableName=RESOURCE_SERVER_POLICY; addColumn tableName=RESOURCE_SERVER_PERM_TICKET; addForeignKeyConstraint baseTableName=RESOURCE_SERVER_PERM_TICKET, constraintName=FK_FRSRPO2128CX4WNKOG82SSRFY, referencedTableName=RESOURCE_SERVER_POLICY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-4.2.0.Final', 'mhajas@redhat.com', 'META-INF/jpa-changelog-authz-4.2.0.Final.xml', '2024-10-23 10:39:07.382101', 61, 'EXECUTED', '9:42a33806f3a0443fe0e7feeec821326c', 'createTable tableName=RESOURCE_URIS; addForeignKeyConstraint baseTableName=RESOURCE_URIS, constraintName=FK_RESOURCE_SERVER_URIS, referencedTableName=RESOURCE_SERVER_RESOURCE; customChange; dropColumn columnName=URI, tableName=RESOURCE_SERVER_RESO...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-4.2.0.Final-KEYCLOAK-9944', 'hmlnarik@redhat.com', 'META-INF/jpa-changelog-authz-4.2.0.Final.xml', '2024-10-23 10:39:07.38608', 62, 'EXECUTED', '9:9968206fca46eecc1f51db9c024bfe56', 'addPrimaryKey constraintName=CONSTRAINT_RESOUR_URIS_PK, tableName=RESOURCE_URIS', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.2.0-KEYCLOAK-6313', 'wadahiro@gmail.com', 'META-INF/jpa-changelog-4.2.0.xml', '2024-10-23 10:39:07.388897', 63, 'EXECUTED', '9:92143a6daea0a3f3b8f598c97ce55c3d', 'addColumn tableName=REQUIRED_ACTION_PROVIDER', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.3.0-KEYCLOAK-7984', 'wadahiro@gmail.com', 'META-INF/jpa-changelog-4.3.0.xml', '2024-10-23 10:39:07.391632', 64, 'EXECUTED', '9:82bab26a27195d889fb0429003b18f40', 'update tableName=REQUIRED_ACTION_PROVIDER', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.6.0-KEYCLOAK-7950', 'psilva@redhat.com', 'META-INF/jpa-changelog-4.6.0.xml', '2024-10-23 10:39:07.394118', 65, 'EXECUTED', '9:e590c88ddc0b38b0ae4249bbfcb5abc3', 'update tableName=RESOURCE_SERVER_RESOURCE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.6.0-KEYCLOAK-8377', 'keycloak', 'META-INF/jpa-changelog-4.6.0.xml', '2024-10-23 10:39:07.43422', 66, 'EXECUTED', '9:5c1f475536118dbdc38d5d7977950cc0', 'createTable tableName=ROLE_ATTRIBUTE; addPrimaryKey constraintName=CONSTRAINT_ROLE_ATTRIBUTE_PK, tableName=ROLE_ATTRIBUTE; addForeignKeyConstraint baseTableName=ROLE_ATTRIBUTE, constraintName=FK_ROLE_ATTRIBUTE_ID, referencedTableName=KEYCLOAK_ROLE...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.6.0-KEYCLOAK-8555', 'gideonray@gmail.com', 'META-INF/jpa-changelog-4.6.0.xml', '2024-10-23 10:39:07.469019', 67, 'EXECUTED', '9:e7c9f5f9c4d67ccbbcc215440c718a17', 'createIndex indexName=IDX_COMPONENT_PROVIDER_TYPE, tableName=COMPONENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.7.0-KEYCLOAK-1267', 'sguilhen@redhat.com', 'META-INF/jpa-changelog-4.7.0.xml', '2024-10-23 10:39:07.473276', 68, 'EXECUTED', '9:88e0bfdda924690d6f4e430c53447dd5', 'addColumn tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.7.0-KEYCLOAK-7275', 'keycloak', 'META-INF/jpa-changelog-4.7.0.xml', '2024-10-23 10:39:07.514696', 69, 'EXECUTED', '9:f53177f137e1c46b6a88c59ec1cb5218', 'renameColumn newColumnName=CREATED_ON, oldColumnName=LAST_SESSION_REFRESH, tableName=OFFLINE_USER_SESSION; addNotNullConstraint columnName=CREATED_ON, tableName=OFFLINE_USER_SESSION; addColumn tableName=OFFLINE_USER_SESSION; customChange; createIn...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('4.8.0-KEYCLOAK-8835', 'sguilhen@redhat.com', 'META-INF/jpa-changelog-4.8.0.xml', '2024-10-23 10:39:07.519829', 70, 'EXECUTED', '9:a74d33da4dc42a37ec27121580d1459f', 'addNotNullConstraint columnName=SSO_MAX_LIFESPAN_REMEMBER_ME, tableName=REALM; addNotNullConstraint columnName=SSO_IDLE_TIMEOUT_REMEMBER_ME, tableName=REALM', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('authz-7.0.0-KEYCLOAK-10443', 'psilva@redhat.com', 'META-INF/jpa-changelog-authz-7.0.0.xml', '2024-10-23 10:39:07.523745', 71, 'EXECUTED', '9:fd4ade7b90c3b67fae0bfcfcb42dfb5f', 'addColumn tableName=RESOURCE_SERVER', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('8.0.0-adding-credential-columns', 'keycloak', 'META-INF/jpa-changelog-8.0.0.xml', '2024-10-23 10:39:07.529902', 72, 'EXECUTED', '9:aa072ad090bbba210d8f18781b8cebf4', 'addColumn tableName=CREDENTIAL; addColumn tableName=FED_USER_CREDENTIAL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('8.0.0-updating-credential-data-not-oracle-fixed', 'keycloak', 'META-INF/jpa-changelog-8.0.0.xml', '2024-10-23 10:39:07.536742', 73, 'EXECUTED', '9:1ae6be29bab7c2aa376f6983b932be37', 'update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=FED_USER_CREDENTIAL; update tableName=FED_USER_CREDENTIAL; update tableName=FED_USER_CREDENTIAL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('8.0.0-updating-credential-data-oracle-fixed', 'keycloak', 'META-INF/jpa-changelog-8.0.0.xml', '2024-10-23 10:39:07.538935', 74, 'MARK_RAN', '9:14706f286953fc9a25286dbd8fb30d97', 'update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=CREDENTIAL; update tableName=FED_USER_CREDENTIAL; update tableName=FED_USER_CREDENTIAL; update tableName=FED_USER_CREDENTIAL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('8.0.0-credential-cleanup-fixed', 'keycloak', 'META-INF/jpa-changelog-8.0.0.xml', '2024-10-23 10:39:07.554283', 75, 'EXECUTED', '9:2b9cc12779be32c5b40e2e67711a218b', 'dropDefaultValue columnName=COUNTER, tableName=CREDENTIAL; dropDefaultValue columnName=DIGITS, tableName=CREDENTIAL; dropDefaultValue columnName=PERIOD, tableName=CREDENTIAL; dropDefaultValue columnName=ALGORITHM, tableName=CREDENTIAL; dropColumn ...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('8.0.0-resource-tag-support', 'keycloak', 'META-INF/jpa-changelog-8.0.0.xml', '2024-10-23 10:39:07.590348', 76, 'EXECUTED', '9:91fa186ce7a5af127a2d7a91ee083cc5', 'addColumn tableName=MIGRATION_MODEL; createIndex indexName=IDX_UPDATE_TIME, tableName=MIGRATION_MODEL', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.0-always-display-client', 'keycloak', 'META-INF/jpa-changelog-9.0.0.xml', '2024-10-23 10:39:07.593643', 77, 'EXECUTED', '9:6335e5c94e83a2639ccd68dd24e2e5ad', 'addColumn tableName=CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.0-drop-constraints-for-column-increase', 'keycloak', 'META-INF/jpa-changelog-9.0.0.xml', '2024-10-23 10:39:07.594979', 78, 'MARK_RAN', '9:6bdb5658951e028bfe16fa0a8228b530', 'dropUniqueConstraint constraintName=UK_FRSR6T700S9V50BU18WS5PMT, tableName=RESOURCE_SERVER_PERM_TICKET; dropUniqueConstraint constraintName=UK_FRSR6T700S9V50BU18WS5HA6, tableName=RESOURCE_SERVER_RESOURCE; dropPrimaryKey constraintName=CONSTRAINT_O...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.0-increase-column-size-federated-fk', 'keycloak', 'META-INF/jpa-changelog-9.0.0.xml', '2024-10-23 10:39:07.607074', 79, 'EXECUTED', '9:d5bc15a64117ccad481ce8792d4c608f', 'modifyDataType columnName=CLIENT_ID, tableName=FED_USER_CONSENT; modifyDataType columnName=CLIENT_REALM_CONSTRAINT, tableName=KEYCLOAK_ROLE; modifyDataType columnName=OWNER, tableName=RESOURCE_SERVER_POLICY; modifyDataType columnName=CLIENT_ID, ta...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.0-recreate-constraints-after-column-increase', 'keycloak', 'META-INF/jpa-changelog-9.0.0.xml', '2024-10-23 10:39:07.608584', 80, 'MARK_RAN', '9:077cba51999515f4d3e7ad5619ab592c', 'addNotNullConstraint columnName=CLIENT_ID, tableName=OFFLINE_CLIENT_SESSION; addNotNullConstraint columnName=OWNER, tableName=RESOURCE_SERVER_PERM_TICKET; addNotNullConstraint columnName=REQUESTER, tableName=RESOURCE_SERVER_PERM_TICKET; addNotNull...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.1-add-index-to-client.client_id', 'keycloak', 'META-INF/jpa-changelog-9.0.1.xml', '2024-10-23 10:39:07.642794', 81, 'EXECUTED', '9:be969f08a163bf47c6b9e9ead8ac2afb', 'createIndex indexName=IDX_CLIENT_ID, tableName=CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.1-KEYCLOAK-12579-drop-constraints', 'keycloak', 'META-INF/jpa-changelog-9.0.1.xml', '2024-10-23 10:39:07.644335', 82, 'MARK_RAN', '9:6d3bb4408ba5a72f39bd8a0b301ec6e3', 'dropUniqueConstraint constraintName=SIBLING_NAMES, tableName=KEYCLOAK_GROUP', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.1-KEYCLOAK-12579-add-not-null-constraint', 'keycloak', 'META-INF/jpa-changelog-9.0.1.xml', '2024-10-23 10:39:07.64858', 83, 'EXECUTED', '9:966bda61e46bebf3cc39518fbed52fa7', 'addNotNullConstraint columnName=PARENT_GROUP, tableName=KEYCLOAK_GROUP', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.1-KEYCLOAK-12579-recreate-constraints', 'keycloak', 'META-INF/jpa-changelog-9.0.1.xml', '2024-10-23 10:39:07.649855', 84, 'MARK_RAN', '9:8dcac7bdf7378e7d823cdfddebf72fda', 'addUniqueConstraint constraintName=SIBLING_NAMES, tableName=KEYCLOAK_GROUP', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('9.0.1-add-index-to-events', 'keycloak', 'META-INF/jpa-changelog-9.0.1.xml', '2024-10-23 10:39:07.684004', 85, 'EXECUTED', '9:7d93d602352a30c0c317e6a609b56599', 'createIndex indexName=IDX_EVENT_TIME, tableName=EVENT_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('map-remove-ri', 'keycloak', 'META-INF/jpa-changelog-11.0.0.xml', '2024-10-23 10:39:07.687979', 86, 'EXECUTED', '9:71c5969e6cdd8d7b6f47cebc86d37627', 'dropForeignKeyConstraint baseTableName=REALM, constraintName=FK_TRAF444KK6QRKMS7N56AIWQ5Y; dropForeignKeyConstraint baseTableName=KEYCLOAK_ROLE, constraintName=FK_KJHO5LE2C0RAL09FL8CM9WFW9', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('map-remove-ri', 'keycloak', 'META-INF/jpa-changelog-12.0.0.xml', '2024-10-23 10:39:07.693543', 87, 'EXECUTED', '9:a9ba7d47f065f041b7da856a81762021', 'dropForeignKeyConstraint baseTableName=REALM_DEFAULT_GROUPS, constraintName=FK_DEF_GROUPS_GROUP; dropForeignKeyConstraint baseTableName=REALM_DEFAULT_ROLES, constraintName=FK_H4WPD7W4HSOOLNI3H0SW7BTJE; dropForeignKeyConstraint baseTableName=CLIENT...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('12.1.0-add-realm-localization-table', 'keycloak', 'META-INF/jpa-changelog-12.0.0.xml', '2024-10-23 10:39:07.698288', 88, 'EXECUTED', '9:fffabce2bc01e1a8f5110d5278500065', 'createTable tableName=REALM_LOCALIZATIONS; addPrimaryKey tableName=REALM_LOCALIZATIONS', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('default-roles', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.703379', 89, 'EXECUTED', '9:fa8a5b5445e3857f4b010bafb5009957', 'addColumn tableName=REALM; customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('default-roles-cleanup', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.708282', 90, 'EXECUTED', '9:67ac3241df9a8582d591c5ed87125f39', 'dropTable tableName=REALM_DEFAULT_ROLES; dropTable tableName=CLIENT_DEFAULT_ROLES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('13.0.0-KEYCLOAK-16844', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.744415', 91, 'EXECUTED', '9:ad1194d66c937e3ffc82386c050ba089', 'createIndex indexName=IDX_OFFLINE_USS_PRELOAD, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('map-remove-ri-13.0.0', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.75065', 92, 'EXECUTED', '9:d9be619d94af5a2f5d07b9f003543b91', 'dropForeignKeyConstraint baseTableName=DEFAULT_CLIENT_SCOPE, constraintName=FK_R_DEF_CLI_SCOPE_SCOPE; dropForeignKeyConstraint baseTableName=CLIENT_SCOPE_CLIENT, constraintName=FK_C_CLI_SCOPE_SCOPE; dropForeignKeyConstraint baseTableName=CLIENT_SC...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('13.0.0-KEYCLOAK-17992-drop-constraints', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.752017', 93, 'MARK_RAN', '9:544d201116a0fcc5a5da0925fbbc3bde', 'dropPrimaryKey constraintName=C_CLI_SCOPE_BIND, tableName=CLIENT_SCOPE_CLIENT; dropIndex indexName=IDX_CLSCOPE_CL, tableName=CLIENT_SCOPE_CLIENT; dropIndex indexName=IDX_CL_CLSCOPE, tableName=CLIENT_SCOPE_CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('13.0.0-increase-column-size-federated', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.757894', 94, 'EXECUTED', '9:43c0c1055b6761b4b3e89de76d612ccf', 'modifyDataType columnName=CLIENT_ID, tableName=CLIENT_SCOPE_CLIENT; modifyDataType columnName=SCOPE_ID, tableName=CLIENT_SCOPE_CLIENT', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('13.0.0-KEYCLOAK-17992-recreate-constraints', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.759656', 95, 'MARK_RAN', '9:8bd711fd0330f4fe980494ca43ab1139', 'addNotNullConstraint columnName=CLIENT_ID, tableName=CLIENT_SCOPE_CLIENT; addNotNullConstraint columnName=SCOPE_ID, tableName=CLIENT_SCOPE_CLIENT; addPrimaryKey constraintName=C_CLI_SCOPE_BIND, tableName=CLIENT_SCOPE_CLIENT; createIndex indexName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('json-string-accomodation-fixed', 'keycloak', 'META-INF/jpa-changelog-13.0.0.xml', '2024-10-23 10:39:07.765783', 96, 'EXECUTED', '9:e07d2bc0970c348bb06fb63b1f82ddbf', 'addColumn tableName=REALM_ATTRIBUTE; update tableName=REALM_ATTRIBUTE; dropColumn columnName=VALUE, tableName=REALM_ATTRIBUTE; renameColumn newColumnName=VALUE, oldColumnName=VALUE_NEW, tableName=REALM_ATTRIBUTE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('14.0.0-KEYCLOAK-11019', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:07.898166', 97, 'EXECUTED', '9:24fb8611e97f29989bea412aa38d12b7', 'createIndex indexName=IDX_OFFLINE_CSS_PRELOAD, tableName=OFFLINE_CLIENT_SESSION; createIndex indexName=IDX_OFFLINE_USS_BY_USER, tableName=OFFLINE_USER_SESSION; createIndex indexName=IDX_OFFLINE_USS_BY_USERSESS, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('14.0.0-KEYCLOAK-18286', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:07.900118', 98, 'MARK_RAN', '9:259f89014ce2506ee84740cbf7163aa7', 'createIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('14.0.0-KEYCLOAK-18286-revert', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:07.913232', 99, 'MARK_RAN', '9:04baaf56c116ed19951cbc2cca584022', 'dropIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('14.0.0-KEYCLOAK-18286-supported-dbs', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:07.967712', 100, 'EXECUTED', '9:60ca84a0f8c94ec8c3504a5a3bc88ee8', 'createIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('14.0.0-KEYCLOAK-18286-unsupported-dbs', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:07.969552', 101, 'MARK_RAN', '9:d3d977031d431db16e2c181ce49d73e9', 'createIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('KEYCLOAK-17267-add-index-to-user-attributes', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:08.02255', 102, 'EXECUTED', '9:0b305d8d1277f3a89a0a53a659ad274c', 'createIndex indexName=IDX_USER_ATTRIBUTE_NAME, tableName=USER_ATTRIBUTE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('KEYCLOAK-18146-add-saml-art-binding-identifier', 'keycloak', 'META-INF/jpa-changelog-14.0.0.xml', '2024-10-23 10:39:08.02849', 103, 'EXECUTED', '9:2c374ad2cdfe20e2905a84c8fac48460', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('15.0.0-KEYCLOAK-18467', 'keycloak', 'META-INF/jpa-changelog-15.0.0.xml', '2024-10-23 10:39:08.036444', 104, 'EXECUTED', '9:47a760639ac597360a8219f5b768b4de', 'addColumn tableName=REALM_LOCALIZATIONS; update tableName=REALM_LOCALIZATIONS; dropColumn columnName=TEXTS, tableName=REALM_LOCALIZATIONS; renameColumn newColumnName=TEXTS, oldColumnName=TEXTS_NEW, tableName=REALM_LOCALIZATIONS; addNotNullConstrai...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('17.0.0-9562', 'keycloak', 'META-INF/jpa-changelog-17.0.0.xml', '2024-10-23 10:39:08.093297', 105, 'EXECUTED', '9:a6272f0576727dd8cad2522335f5d99e', 'createIndex indexName=IDX_USER_SERVICE_ACCOUNT, tableName=USER_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('18.0.0-10625-IDX_ADMIN_EVENT_TIME', 'keycloak', 'META-INF/jpa-changelog-18.0.0.xml', '2024-10-23 10:39:08.156165', 106, 'EXECUTED', '9:015479dbd691d9cc8669282f4828c41d', 'createIndex indexName=IDX_ADMIN_EVENT_TIME, tableName=ADMIN_EVENT_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('18.0.15-30992-index-consent', 'keycloak', 'META-INF/jpa-changelog-18.0.15.xml', '2024-10-23 10:39:08.229975', 107, 'EXECUTED', '9:80071ede7a05604b1f4906f3bf3b00f0', 'createIndex indexName=IDX_USCONSENT_SCOPE_ID, tableName=USER_CONSENT_CLIENT_SCOPE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('19.0.0-10135', 'keycloak', 'META-INF/jpa-changelog-19.0.0.xml', '2024-10-23 10:39:08.235613', 108, 'EXECUTED', '9:9518e495fdd22f78ad6425cc30630221', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('20.0.0-12964-supported-dbs', 'keycloak', 'META-INF/jpa-changelog-20.0.0.xml', '2024-10-23 10:39:08.285606', 109, 'EXECUTED', '9:e5f243877199fd96bcc842f27a1656ac', 'createIndex indexName=IDX_GROUP_ATT_BY_NAME_VALUE, tableName=GROUP_ATTRIBUTE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('20.0.0-12964-unsupported-dbs', 'keycloak', 'META-INF/jpa-changelog-20.0.0.xml', '2024-10-23 10:39:08.287714', 110, 'MARK_RAN', '9:1a6fcaa85e20bdeae0a9ce49b41946a5', 'createIndex indexName=IDX_GROUP_ATT_BY_NAME_VALUE, tableName=GROUP_ATTRIBUTE', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('client-attributes-string-accomodation-fixed', 'keycloak', 'META-INF/jpa-changelog-20.0.0.xml', '2024-10-23 10:39:08.294374', 111, 'EXECUTED', '9:3f332e13e90739ed0c35b0b25b7822ca', 'addColumn tableName=CLIENT_ATTRIBUTES; update tableName=CLIENT_ATTRIBUTES; dropColumn columnName=VALUE, tableName=CLIENT_ATTRIBUTES; renameColumn newColumnName=VALUE, oldColumnName=VALUE_NEW, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('21.0.2-17277', 'keycloak', 'META-INF/jpa-changelog-21.0.2.xml', '2024-10-23 10:39:08.298214', 112, 'EXECUTED', '9:7ee1f7a3fb8f5588f171fb9a6ab623c0', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('21.1.0-19404', 'keycloak', 'META-INF/jpa-changelog-21.1.0.xml', '2024-10-23 10:39:08.308231', 113, 'EXECUTED', '9:3d7e830b52f33676b9d64f7f2b2ea634', 'modifyDataType columnName=DECISION_STRATEGY, tableName=RESOURCE_SERVER_POLICY; modifyDataType columnName=LOGIC, tableName=RESOURCE_SERVER_POLICY; modifyDataType columnName=POLICY_ENFORCE_MODE, tableName=RESOURCE_SERVER', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('21.1.0-19404-2', 'keycloak', 'META-INF/jpa-changelog-21.1.0.xml', '2024-10-23 10:39:08.311449', 114, 'MARK_RAN', '9:627d032e3ef2c06c0e1f73d2ae25c26c', 'addColumn tableName=RESOURCE_SERVER_POLICY; update tableName=RESOURCE_SERVER_POLICY; dropColumn columnName=DECISION_STRATEGY, tableName=RESOURCE_SERVER_POLICY; renameColumn newColumnName=DECISION_STRATEGY, oldColumnName=DECISION_STRATEGY_NEW, tabl...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('22.0.0-17484-updated', 'keycloak', 'META-INF/jpa-changelog-22.0.0.xml', '2024-10-23 10:39:08.316311', 115, 'EXECUTED', '9:90af0bfd30cafc17b9f4d6eccd92b8b3', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('22.0.5-24031', 'keycloak', 'META-INF/jpa-changelog-22.0.0.xml', '2024-10-23 10:39:08.318071', 116, 'MARK_RAN', '9:a60d2d7b315ec2d3eba9e2f145f9df28', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('23.0.0-12062', 'keycloak', 'META-INF/jpa-changelog-23.0.0.xml', '2024-10-23 10:39:08.323708', 117, 'EXECUTED', '9:2168fbe728fec46ae9baf15bf80927b8', 'addColumn tableName=COMPONENT_CONFIG; update tableName=COMPONENT_CONFIG; dropColumn columnName=VALUE, tableName=COMPONENT_CONFIG; renameColumn newColumnName=VALUE, oldColumnName=VALUE_NEW, tableName=COMPONENT_CONFIG', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('23.0.0-17258', 'keycloak', 'META-INF/jpa-changelog-23.0.0.xml', '2024-10-23 10:39:08.326864', 118, 'EXECUTED', '9:36506d679a83bbfda85a27ea1864dca8', 'addColumn tableName=EVENT_ENTITY', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.0-9758', 'keycloak', 'META-INF/jpa-changelog-24.0.0.xml', '2024-10-23 10:39:08.46949', 119, 'EXECUTED', '9:502c557a5189f600f0f445a9b49ebbce', 'addColumn tableName=USER_ATTRIBUTE; addColumn tableName=FED_USER_ATTRIBUTE; createIndex indexName=USER_ATTR_LONG_VALUES, tableName=USER_ATTRIBUTE; createIndex indexName=FED_USER_ATTR_LONG_VALUES, tableName=FED_USER_ATTRIBUTE; createIndex indexName...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.0-9758-2', 'keycloak', 'META-INF/jpa-changelog-24.0.0.xml', '2024-10-23 10:39:08.474803', 120, 'EXECUTED', '9:bf0fdee10afdf597a987adbf291db7b2', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.0-26618-drop-index-if-present', 'keycloak', 'META-INF/jpa-changelog-24.0.0.xml', '2024-10-23 10:39:08.481673', 121, 'MARK_RAN', '9:04baaf56c116ed19951cbc2cca584022', 'dropIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.0-26618-reindex', 'keycloak', 'META-INF/jpa-changelog-24.0.0.xml', '2024-10-23 10:39:08.520868', 122, 'EXECUTED', '9:08707c0f0db1cef6b352db03a60edc7f', 'createIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.2-27228', 'keycloak', 'META-INF/jpa-changelog-24.0.2.xml', '2024-10-23 10:39:08.52476', 123, 'EXECUTED', '9:eaee11f6b8aa25d2cc6a84fb86fc6238', 'customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.2-27967-drop-index-if-present', 'keycloak', 'META-INF/jpa-changelog-24.0.2.xml', '2024-10-23 10:39:08.526195', 124, 'MARK_RAN', '9:04baaf56c116ed19951cbc2cca584022', 'dropIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('24.0.2-27967-reindex', 'keycloak', 'META-INF/jpa-changelog-24.0.2.xml', '2024-10-23 10:39:08.528105', 125, 'MARK_RAN', '9:d3d977031d431db16e2c181ce49d73e9', 'createIndex indexName=IDX_CLIENT_ATT_BY_NAME_VALUE, tableName=CLIENT_ATTRIBUTES', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-tables', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.534113', 126, 'EXECUTED', '9:deda2df035df23388af95bbd36c17cef', 'addColumn tableName=OFFLINE_USER_SESSION; addColumn tableName=OFFLINE_CLIENT_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-creation', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.571493', 127, 'EXECUTED', '9:3e96709818458ae49f3c679ae58d263a', 'createIndex indexName=IDX_OFFLINE_USS_BY_LAST_SESSION_REFRESH, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-cleanup', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.577342', 128, 'EXECUTED', '9:8c0cfa341a0474385b324f5c4b2dfcc1', 'dropIndex indexName=IDX_OFFLINE_USS_CREATEDON, tableName=OFFLINE_USER_SESSION; dropIndex indexName=IDX_OFFLINE_USS_PRELOAD, tableName=OFFLINE_USER_SESSION; dropIndex indexName=IDX_OFFLINE_USS_BY_USERSESS, tableName=OFFLINE_USER_SESSION; dropIndex ...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-2-mysql', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.5791', 129, 'MARK_RAN', '9:b7ef76036d3126bb83c2423bf4d449d6', 'createIndex indexName=IDX_OFFLINE_USS_BY_BROKER_SESSION_ID, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-2-not-mysql', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.614721', 130, 'EXECUTED', '9:23396cf51ab8bc1ae6f0cac7f9f6fcf7', 'createIndex indexName=IDX_OFFLINE_USS_BY_BROKER_SESSION_ID, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-org', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.624822', 131, 'EXECUTED', '9:5c859965c2c9b9c72136c360649af157', 'createTable tableName=ORG; addUniqueConstraint constraintName=UK_ORG_NAME, tableName=ORG; addUniqueConstraint constraintName=UK_ORG_GROUP, tableName=ORG; createTable tableName=ORG_DOMAIN', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('unique-consentuser', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.632264', 132, 'EXECUTED', '9:5857626a2ea8767e9a6c66bf3a2cb32f', 'customChange; dropUniqueConstraint constraintName=UK_JKUWUVD56ONTGSUHOGM8UEWRT, tableName=USER_CONSENT; addUniqueConstraint constraintName=UK_LOCAL_CONSENT, tableName=USER_CONSENT; addUniqueConstraint constraintName=UK_EXTERNAL_CONSENT, tableName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('unique-consentuser-mysql', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.633938', 133, 'MARK_RAN', '9:b79478aad5adaa1bc428e31563f55e8e', 'customChange; dropUniqueConstraint constraintName=UK_JKUWUVD56ONTGSUHOGM8UEWRT, tableName=USER_CONSENT; addUniqueConstraint constraintName=UK_LOCAL_CONSENT, tableName=USER_CONSENT; addUniqueConstraint constraintName=UK_EXTERNAL_CONSENT, tableName=...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28861-index-creation', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-10-23 10:39:08.697439', 134, 'EXECUTED', '9:b9acb58ac958d9ada0fe12a5d4794ab1', 'createIndex indexName=IDX_PERM_TICKET_REQUESTER, tableName=RESOURCE_SERVER_PERM_TICKET; createIndex indexName=IDX_PERM_TICKET_OWNER, tableName=RESOURCE_SERVER_PERM_TICKET', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-org-alias', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.702772', 135, 'EXECUTED', '9:6ef7d63e4412b3c2d66ed179159886a4', 'addColumn tableName=ORG; update tableName=ORG; addNotNullConstraint columnName=ALIAS, tableName=ORG; addUniqueConstraint constraintName=UK_ORG_ALIAS, tableName=ORG', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-org-group', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.709036', 136, 'EXECUTED', '9:da8e8087d80ef2ace4f89d8c5b9ca223', 'addColumn tableName=KEYCLOAK_GROUP; update tableName=KEYCLOAK_GROUP; addNotNullConstraint columnName=TYPE, tableName=KEYCLOAK_GROUP; customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-org-indexes', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.741748', 137, 'EXECUTED', '9:79b05dcd610a8c7f25ec05135eec0857', 'createIndex indexName=IDX_ORG_DOMAIN_ORG_ID, tableName=ORG_DOMAIN', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-org-group-membership', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.746412', 138, 'EXECUTED', '9:a6ace2ce583a421d89b01ba2a28dc2d4', 'addColumn tableName=USER_GROUP_MEMBERSHIP; update tableName=USER_GROUP_MEMBERSHIP; addNotNullConstraint columnName=MEMBERSHIP_TYPE, tableName=USER_GROUP_MEMBERSHIP', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('31296-persist-revoked-access-tokens', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.750745', 139, 'EXECUTED', '9:64ef94489d42a358e8304b0e245f0ed4', 'createTable tableName=REVOKED_TOKEN; addPrimaryKey constraintName=CONSTRAINT_RT, tableName=REVOKED_TOKEN', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('31725-index-persist-revoked-access-tokens', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.782772', 140, 'EXECUTED', '9:b994246ec2bf7c94da881e1d28782c7b', 'createIndex indexName=IDX_REV_TOKEN_ON_EXPIRE, tableName=REVOKED_TOKEN', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-idps-for-login', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.851003', 141, 'EXECUTED', '9:51f5fffadf986983d4bd59582c6c1604', 'addColumn tableName=IDENTITY_PROVIDER; createIndex indexName=IDX_IDP_REALM_ORG, tableName=IDENTITY_PROVIDER; createIndex indexName=IDX_IDP_FOR_LOGIN, tableName=IDENTITY_PROVIDER; customChange', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0-32583-drop-redundant-index-on-client-session', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.907803', 142, 'EXECUTED', '9:24972d83bf27317a055d234187bb4af9', 'dropIndex indexName=IDX_US_SESS_ID_ON_CL_SESS, tableName=OFFLINE_CLIENT_SESSION', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.0.32582-remove-tables-user-session-user-session-note-and-client-session', 'keycloak', 'META-INF/jpa-changelog-26.0.0.xml', '2024-10-23 10:39:08.919226', 143, 'EXECUTED', '9:febdc0f47f2ed241c59e60f58c3ceea5', 'dropTable tableName=CLIENT_SESSION_ROLE; dropTable tableName=CLIENT_SESSION_NOTE; dropTable tableName=CLIENT_SESSION_PROT_MAPPER; dropTable tableName=CLIENT_SESSION_AUTH_STATUS; dropTable tableName=CLIENT_USER_SESSION_NOTE; dropTable tableName=CLI...', '', NULL, '4.29.1', NULL, NULL, '9679943312');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-cleanup-uss-createdon', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-12-09 08:31:03.40603', 145, 'MARK_RAN', '9:78ab4fc129ed5e8265dbcc3485fba92f', 'dropIndex indexName=IDX_OFFLINE_USS_CREATEDON, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '3733063333');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-cleanup-uss-preload', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-12-09 08:31:03.505962', 146, 'MARK_RAN', '9:de5f7c1f7e10994ed8b62e621d20eaab', 'dropIndex indexName=IDX_OFFLINE_USS_PRELOAD, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '3733063333');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-cleanup-uss-by-usersess', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-12-09 08:31:03.538985', 147, 'MARK_RAN', '9:6eee220d024e38e89c799417ec33667f', 'dropIndex indexName=IDX_OFFLINE_USS_BY_USERSESS, tableName=OFFLINE_USER_SESSION', '', NULL, '4.29.1', NULL, NULL, '3733063333');
INSERT INTO "public"."databasechangelog" VALUES ('25.0.0-28265-index-cleanup-css-preload', 'keycloak', 'META-INF/jpa-changelog-25.0.0.xml', '2024-12-09 08:31:03.573575', 148, 'MARK_RAN', '9:5411d2fb2891d3e8d63ddb55dfa3c0c9', 'dropIndex indexName=IDX_OFFLINE_CSS_PRELOAD, tableName=OFFLINE_CLIENT_SESSION', '', NULL, '4.29.1', NULL, NULL, '3733063333');
INSERT INTO "public"."databasechangelog" VALUES ('26.0.6-34013', 'keycloak', 'META-INF/jpa-changelog-26.0.6.xml', '2024-12-09 08:31:03.610044', 149, 'EXECUTED', '9:e6b686a15759aef99a6d758a5c4c6a26', 'addColumn tableName=ADMIN_EVENT_ENTITY', '', NULL, '4.29.1', NULL, NULL, '3733063333');

-- ----------------------------
-- Table structure for databasechangeloglock
-- ----------------------------
DROP TABLE IF EXISTS "public"."databasechangeloglock";
CREATE TABLE "public"."databasechangeloglock" (
  "id" int4 NOT NULL,
  "locked" bool NOT NULL,
  "lockgranted" timestamp(6),
  "lockedby" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of databasechangeloglock
-- ----------------------------
INSERT INTO "public"."databasechangeloglock" VALUES (1, 'f', NULL, NULL);
INSERT INTO "public"."databasechangeloglock" VALUES (1000, 'f', NULL, NULL);

-- ----------------------------
-- Table structure for default_client_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."default_client_scope";
CREATE TABLE "public"."default_client_scope" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "default_scope" bool NOT NULL DEFAULT false
)
;

-- ----------------------------
-- Records of default_client_scope
-- ----------------------------
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '53331029-d6f6-4c1e-a44a-b651c6b7f35a', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'e760b92c-cc89-486a-893d-9c0d5f792170', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'fab7a1a9-abaa-4598-8a28-e426140649ee', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '37b9772b-4a05-4b69-8f26-9c1ef1062650', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '955bf38a-c128-41db-b0b5-780cb1b6376d', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0c85b1c7-6314-4a0b-b5fb-b76308dbab56', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0127c5fb-e845-40dc-b964-70639f5105d9', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '1da7a744-af7f-4da8-86d6-c4a8f49bed77', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '19990782-99f3-4f22-a383-0c4832e4f781', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '8b71be46-e97e-4fb6-8044-ab9e91be71b2', 't');
INSERT INTO "public"."default_client_scope" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '8a1ce846-12c2-43ab-b220-93dcf015f2b6', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '27d726f0-a771-44c8-8db7-e7a44329b42f', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '0dc62ecf-9c4c-40fb-ba9a-4725abf1d3ff', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f7db029c-7313-47c1-a6e8-56050682927f', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '19b9d4dd-3607-4e3a-838e-b156630fe78e', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'aab92fd1-d7b8-456c-aa9f-19c6c782260c', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'cca2f7fe-1d61-468e-a9df-83d25f108dc2', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'ad9286f6-2377-4db7-872b-5edcbef2017a', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '147d0a04-66fc-49db-a1c4-fa233eb47825', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'fcc54556-ec96-4011-a89e-7c1d0ea2e714', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '80edc885-da5f-472c-90cc-d8b0e6d1f011', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '9475e044-78d6-41ac-88a8-0cc0cedf5875', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'e5d6dd73-37ab-4864-abd8-b473bc110772', 'f');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '2c26c6cb-b18b-4fd9-bbde-38d81cfaa038', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '4e44f85d-bb73-4eb9-af2a-c1a641792a94', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '51ec41ba-ea8b-4359-80a3-e3de154ee389', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec', 't');
INSERT INTO "public"."default_client_scope" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '804408e1-e065-4362-8cd1-414c9b9777b3', 't');

-- ----------------------------
-- Table structure for event_entity
-- ----------------------------
DROP TABLE IF EXISTS "public"."event_entity";
CREATE TABLE "public"."event_entity" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "client_id" varchar(255) COLLATE "pg_catalog"."default",
  "details_json" varchar(2550) COLLATE "pg_catalog"."default",
  "error" varchar(255) COLLATE "pg_catalog"."default",
  "ip_address" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(255) COLLATE "pg_catalog"."default",
  "session_id" varchar(255) COLLATE "pg_catalog"."default",
  "event_time" int8,
  "type" varchar(255) COLLATE "pg_catalog"."default",
  "user_id" varchar(255) COLLATE "pg_catalog"."default",
  "details_json_long_value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of event_entity
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_attribute";
CREATE TABLE "public"."fed_user_attribute" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default",
  "value" varchar(2024) COLLATE "pg_catalog"."default",
  "long_value_hash" bytea,
  "long_value_hash_lower_case" bytea,
  "long_value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of fed_user_attribute
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_consent
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_consent";
CREATE TABLE "public"."fed_user_consent" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "client_id" varchar(255) COLLATE "pg_catalog"."default",
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default",
  "created_date" int8,
  "last_updated_date" int8,
  "client_storage_provider" varchar(36) COLLATE "pg_catalog"."default",
  "external_client_id" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of fed_user_consent
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_consent_cl_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_consent_cl_scope";
CREATE TABLE "public"."fed_user_consent_cl_scope" (
  "user_consent_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of fed_user_consent_cl_scope
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_credential
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_credential";
CREATE TABLE "public"."fed_user_credential" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "salt" bytea,
  "type" varchar(255) COLLATE "pg_catalog"."default",
  "created_date" int8,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default",
  "user_label" varchar(255) COLLATE "pg_catalog"."default",
  "secret_data" text COLLATE "pg_catalog"."default",
  "credential_data" text COLLATE "pg_catalog"."default",
  "priority" int4
)
;

-- ----------------------------
-- Records of fed_user_credential
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_group_membership
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_group_membership";
CREATE TABLE "public"."fed_user_group_membership" (
  "group_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of fed_user_group_membership
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_required_action
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_required_action";
CREATE TABLE "public"."fed_user_required_action" (
  "required_action" varchar(255) COLLATE "pg_catalog"."default" NOT NULL DEFAULT ' '::character varying,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of fed_user_required_action
-- ----------------------------

-- ----------------------------
-- Table structure for fed_user_role_mapping
-- ----------------------------
DROP TABLE IF EXISTS "public"."fed_user_role_mapping";
CREATE TABLE "public"."fed_user_role_mapping" (
  "role_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of fed_user_role_mapping
-- ----------------------------

-- ----------------------------
-- Table structure for federated_identity
-- ----------------------------
DROP TABLE IF EXISTS "public"."federated_identity";
CREATE TABLE "public"."federated_identity" (
  "identity_provider" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "federated_user_id" varchar(255) COLLATE "pg_catalog"."default",
  "federated_username" varchar(255) COLLATE "pg_catalog"."default",
  "token" text COLLATE "pg_catalog"."default",
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of federated_identity
-- ----------------------------

-- ----------------------------
-- Table structure for federated_user
-- ----------------------------
DROP TABLE IF EXISTS "public"."federated_user";
CREATE TABLE "public"."federated_user" (
  "id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "storage_provider_id" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of federated_user
-- ----------------------------

-- ----------------------------
-- Table structure for group_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."group_attribute";
CREATE TABLE "public"."group_attribute" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'sybase-needs-something-here'::character varying,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "group_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of group_attribute
-- ----------------------------

-- ----------------------------
-- Table structure for group_role_mapping
-- ----------------------------
DROP TABLE IF EXISTS "public"."group_role_mapping";
CREATE TABLE "public"."group_role_mapping" (
  "role_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "group_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of group_role_mapping
-- ----------------------------

-- ----------------------------
-- Table structure for identity_provider
-- ----------------------------
DROP TABLE IF EXISTS "public"."identity_provider";
CREATE TABLE "public"."identity_provider" (
  "internal_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "enabled" bool NOT NULL DEFAULT false,
  "provider_alias" varchar(255) COLLATE "pg_catalog"."default",
  "provider_id" varchar(255) COLLATE "pg_catalog"."default",
  "store_token" bool NOT NULL DEFAULT false,
  "authenticate_by_default" bool NOT NULL DEFAULT false,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "add_token_role" bool NOT NULL DEFAULT true,
  "trust_email" bool NOT NULL DEFAULT false,
  "first_broker_login_flow_id" varchar(36) COLLATE "pg_catalog"."default",
  "post_broker_login_flow_id" varchar(36) COLLATE "pg_catalog"."default",
  "provider_display_name" varchar(255) COLLATE "pg_catalog"."default",
  "link_only" bool NOT NULL DEFAULT false,
  "organization_id" varchar(255) COLLATE "pg_catalog"."default",
  "hide_on_login" bool DEFAULT false
)
;

-- ----------------------------
-- Records of identity_provider
-- ----------------------------

-- ----------------------------
-- Table structure for identity_provider_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."identity_provider_config";
CREATE TABLE "public"."identity_provider_config" (
  "identity_provider_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of identity_provider_config
-- ----------------------------

-- ----------------------------
-- Table structure for identity_provider_mapper
-- ----------------------------
DROP TABLE IF EXISTS "public"."identity_provider_mapper";
CREATE TABLE "public"."identity_provider_mapper" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "idp_alias" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "idp_mapper_name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of identity_provider_mapper
-- ----------------------------

-- ----------------------------
-- Table structure for idp_mapper_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."idp_mapper_config";
CREATE TABLE "public"."idp_mapper_config" (
  "idp_mapper_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of idp_mapper_config
-- ----------------------------

-- ----------------------------
-- Table structure for keycloak_group
-- ----------------------------
DROP TABLE IF EXISTS "public"."keycloak_group";
CREATE TABLE "public"."keycloak_group" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "parent_group" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "type" int4 NOT NULL DEFAULT 0
)
;

-- ----------------------------
-- Records of keycloak_group
-- ----------------------------

-- ----------------------------
-- Table structure for keycloak_role
-- ----------------------------
DROP TABLE IF EXISTS "public"."keycloak_role";
CREATE TABLE "public"."keycloak_role" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "client_realm_constraint" varchar(255) COLLATE "pg_catalog"."default",
  "client_role" bool NOT NULL DEFAULT false,
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(255) COLLATE "pg_catalog"."default",
  "client" varchar(36) COLLATE "pg_catalog"."default",
  "realm" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of keycloak_role
-- ----------------------------
INSERT INTO "public"."keycloak_role" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', '${role_default-roles}', 'default-roles-master', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2fb3af6d-3c15-45b3-a116-74e376e1d09e', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', '${role_create-realm}', 'create-realm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', '${role_admin}', 'admin', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('060b589b-7b85-4608-8862-a043fa0a03ad', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_create-client}', 'create-client', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('9483ad2e-60d1-4b15-8266-cf6d559b0f7c', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-realm}', 'view-realm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2d2f1b88-8ba1-4744-bc39-fea22ede194d', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-users}', 'view-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('aadf1bdc-8e66-4a0b-a0e2-c4897de78357', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-clients}', 'view-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('df7ffc8b-2ac0-41ce-b0e3-3c6c4de4eedc', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-events}', 'view-events', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('d8b3b296-0ad0-4f48-90e3-1c043bb6e1ec', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-identity-providers}', 'view-identity-providers', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('bac80184-b8e1-4099-a08d-54d06499c34f', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_view-authorization}', 'view-authorization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2aeb0a70-e557-4674-8e85-110a0a36af8f', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-realm}', 'manage-realm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('b65d5f18-203c-4358-a0f1-ec664f30abdd', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-users}', 'manage-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('3f2ad7e1-a700-41df-8a97-70eae31b74f3', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-clients}', 'manage-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('81ad7669-7a27-4e5f-8477-68b030404611', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-events}', 'manage-events', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('c34e146b-6303-4420-aba5-8716ec94f831', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-identity-providers}', 'manage-identity-providers', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('f07d7b23-ca05-4ce4-940d-e8fcf8f4341d', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_manage-authorization}', 'manage-authorization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('272ab104-177b-4a70-9d3c-3fe48a527e5b', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_query-users}', 'query-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('4bb7a5f2-1770-4a13-b4bb-fb140f38c1f8', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_query-clients}', 'query-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2e9ad6a3-001c-4980-a820-c9283691e44a', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_query-realms}', 'query-realms', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('39516407-2312-480b-87e4-c21c1e85adf4', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_query-groups}', 'query-groups', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('80a9c87e-0298-43c6-9b83-a434aff9242e', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_view-profile}', 'view-profile', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('6b07fca1-3f3a-438d-821b-4236995a1d27', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_manage-account}', 'manage-account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('020ebde6-eef1-4cd3-aff3-88981f134819', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_manage-account-links}', 'manage-account-links', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('5d588d65-59ce-4f0b-991c-49a1bdd1e325', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_view-applications}', 'view-applications', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('654677e4-2e8c-4b64-9ec3-2c04f88cbcf2', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_view-consent}', 'view-consent', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('542f5bc4-40ce-4913-98fb-79ba23afe6af', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_manage-consent}', 'manage-consent', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('984612d6-0d73-4daa-af18-8ba04bd98970', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_view-groups}', 'view-groups', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('593340da-5cda-43f9-a8cd-f6b8c48a744f', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', 't', '${role_delete-account}', 'delete-account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9a5a698a-2bdf-431c-893c-cea1ca8d7218', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('8fd5e2b4-1f07-450f-88a1-3f497cfc0b9b', '9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', 't', '${role_read-token}', 'read-token', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '9e94303a-44e9-48f9-9c2e-f8bf2a8c608d', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('78604774-c1b6-41a6-8807-f86fb24b21fa', 'b478982d-7f85-498c-8b83-2903d6c1116a', 't', '${role_impersonation}', 'impersonation', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'b478982d-7f85-498c-8b83-2903d6c1116a', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('80b2ebd4-bd1e-4449-b497-535ba5838c40', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', '${role_offline-access}', 'offline_access', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('562f2fa0-5959-471d-b863-a648cfba36aa', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', '${role_uma_authorization}', 'uma_authorization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f', '${role_default-roles}', 'default-roles-supos', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('1b77aefe-76bf-4abb-8497-dff7eaa27ae9', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_create-client}', 'create-client', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('94e998e3-56eb-4041-8636-5403ef374959', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-realm}', 'view-realm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('ed36401b-28bd-4b41-928f-c9c408a91760', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-users}', 'view-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('c80893f2-a0c1-44c3-92a4-13e1727ac3e3', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-clients}', 'view-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('a36edfae-7ae3-4b91-a3da-2ca591819d9c', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-events}', 'view-events', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('864302b3-59bc-4849-9ae2-72d08b527be0', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-identity-providers}', 'view-identity-providers', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('264402a2-05d4-4a20-9d6b-6463df42354b', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_view-authorization}', 'view-authorization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('d4d80f10-f129-4201-bc25-85a676f76af6', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-realm}', 'manage-realm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2ee8df51-85c0-4b5e-82bc-fe69f84bdba3', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-users}', 'manage-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('e715a939-c828-44db-a29f-d5dc830dd735', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-clients}', 'manage-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('223fbdd1-640c-4e78-b059-fbe6ef340c1f', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-events}', 'manage-events', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('c3aec23b-c2c7-4daa-80a5-62f3d9c421d9', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-identity-providers}', 'manage-identity-providers', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('d5228208-c92b-4a39-9053-db2827876ad6', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_manage-authorization}', 'manage-authorization', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('a8ac2545-ae16-444d-8630-47f9212f1c66', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_query-users}', 'query-users', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('d7a520fa-2195-4da4-8c83-d26940a02be3', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_query-clients}', 'query-clients', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('f2d71319-f501-4a02-ad64-d2515ca5964a', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_query-realms}', 'query-realms', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('01e8a94c-36b3-41c5-869b-a4b8d9f46c2c', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_query-groups}', 'query-groups', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('0ea1ac02-1b5a-4ad2-8467-1596b534d603', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_realm-admin}', 'realm-admin', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('a54b0c3e-63be-4650-9645-24f079ccad67', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_create-client}', 'create-client', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('dc731ee5-13ea-4530-966a-5657179d3054', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-realm}', 'view-realm', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('edac0850-db9a-4eac-8b34-3b046ecfea41', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-users}', 'view-users', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('801d0c19-5995-4bdf-bd18-7c3e319af993', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-clients}', 'view-clients', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('e211771c-a307-4607-b284-66fe1fab0bd1', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-events}', 'view-events', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('76aaa660-3611-46a3-8c8e-1d0c81c345a2', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-identity-providers}', 'view-identity-providers', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('7375196b-bd3e-4352-a70b-942cc885b0e9', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_view-authorization}', 'view-authorization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('ea50dad8-316b-4e11-830e-c59f57bd1ba9', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-realm}', 'manage-realm', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('52e39f80-cd4e-4469-8683-383cd80b6f69', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-users}', 'manage-users', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('36e554a4-010a-4e99-b10f-72a1fe910f18', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-clients}', 'manage-clients', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('a3cef62a-a518-4772-a27f-bab74d1aa5e4', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-events}', 'manage-events', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('44312bf7-797b-4ad1-86c6-01126d16fb8f', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-identity-providers}', 'manage-identity-providers', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('bc870077-52cf-4c18-b309-646353e532f3', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_manage-authorization}', 'manage-authorization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('eb9eb0bd-b100-40a2-bba3-fee42a156d0b', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_query-users}', 'query-users', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('bba48ce9-71db-4013-a524-121b4410b4a9', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_query-clients}', 'query-clients', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('045f3abc-30ce-4419-b1b8-d648764bf324', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_query-realms}', 'query-realms', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('5ade70e4-7643-497e-b0bf-0746538c609a', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_query-groups}', 'query-groups', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('32efd528-0e19-4c16-b654-fd3f80a824e7', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_view-profile}', 'view-profile', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('f9190462-32f2-4183-a192-20c1525b9b1a', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_manage-account}', 'manage-account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('7b245f39-d8e4-4186-bc14-5b4561139968', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_manage-account-links}', 'manage-account-links', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('ed0e5ab3-3b89-4625-86be-e2f405638793', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_view-applications}', 'view-applications', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('eff80418-df2b-407f-8589-6d4856f10fd1', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_view-consent}', 'view-consent', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('020a4c2e-a65d-4eb9-9c92-7a238913654c', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_manage-consent}', 'manage-consent', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('954a1660-6dc1-4ae5-b6f7-d2706bed7df2', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_view-groups}', 'view-groups', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('7d347427-5196-476b-b265-caa86c3d6ff9', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', 't', '${role_delete-account}', 'delete-account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'dc2e7749-eb5c-4249-ae0a-40abd10990a7', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('75e93375-d75f-454d-b9f3-7a9c46fc02d2', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 't', '${role_impersonation}', 'impersonation', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('09e0c927-c268-4f7e-af09-a9c46a413910', '1e143276-845b-4159-ad6b-1817ec62204c', 't', '${role_impersonation}', 'impersonation', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1e143276-845b-4159-ad6b-1817ec62204c', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('18ea01e0-8942-4c4c-9c06-6cfb0946517a', '5b6e7278-a3a8-407d-94eb-6befd126bf16', 't', '${role_read-token}', 'read-token', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5b6e7278-a3a8-407d-94eb-6befd126bf16', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('c4cc1aab-5a86-4697-a33e-4976807b8563', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f', '${role_offline-access}', 'offline_access', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('75586479-0c1a-458b-a3d8-6486ad42e7ad', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f', '${role_uma_authorization}', 'uma_authorization', '8920b375-d705-4d30-8a71-52d9c14ec4ba', NULL, NULL);
INSERT INTO "public"."keycloak_role" VALUES ('625d093d-1333-47d4-92fa-dded93a4f90a', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', '', 'shimu', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('831f62ab-d306-4b11-882e-b23c37ee8c7e', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', NULL, 'uma_protection', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('a22ce15f-7bef-4e2e-9909-78f51b91c799', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', '管理员', 'admin', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('71dd6dc2-6b12-4273-9ec0-b44b86e5b500', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', '普通用户', 'normal-user', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('7ca9f922-0d35-44cf-8747-8dcfd5e66f8e', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', '超级管理员', 'super-admin', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('00e0b0d2-ba36-4927-ac5d-15c4548389f2', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', 'deny-role', 'deny-role', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('2152d19d-e4f9-488d-8509-e49cf239596a', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', 'supos-default', 'supos-default', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."keycloak_role" VALUES ('c5921f89-9745-4c8a-9c69-b3015c94f2ea', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', 'ldap-initialized', 'ldap-initialized', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);


-- ----------------------------
-- Table structure for migration_model
-- ----------------------------
DROP TABLE IF EXISTS "public"."migration_model";
CREATE TABLE "public"."migration_model" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "version" varchar(36) COLLATE "pg_catalog"."default",
  "update_time" int8 NOT NULL DEFAULT 0
)
;

-- ----------------------------
-- Records of migration_model
-- ----------------------------
INSERT INTO "public"."migration_model" VALUES ('90jhc', '26.0.1', 1729679949);
INSERT INTO "public"."migration_model" VALUES ('snsi2', '26.0.5', 1731910076);
INSERT INTO "public"."migration_model" VALUES ('9za6w', '26.0.7', 1733733063);
INSERT INTO "public"."migration_model" VALUES ('iuiua', '26.0.8', 1743402888);

-- ----------------------------
-- Table structure for offline_client_session
-- ----------------------------
DROP TABLE IF EXISTS "public"."offline_client_session";
CREATE TABLE "public"."offline_client_session" (
  "user_session_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "client_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "offline_flag" varchar(4) COLLATE "pg_catalog"."default" NOT NULL,
  "timestamp" int4,
  "data" text COLLATE "pg_catalog"."default",
  "client_storage_provider" varchar(36) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'local'::character varying,
  "external_client_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'local'::character varying,
  "version" int4 DEFAULT 0
)
;

-- ----------------------------
-- Records of offline_client_session
-- ----------------------------
INSERT INTO "public"."offline_client_session" VALUES ('ddfa9a6e-026a-4960-bbd5-32b08a6bf2c6', '5d654a79-6353-4eba-8f64-a0a7c812e454', '0', 1734058412, '{"authMethod":"openid-connect","redirectUri":"KEYCLOAK_BASE_URL_VAR/keycloak/home/auth/admin/master/console/","notes":{"clientId":"5d654a79-6353-4eba-8f64-a0a7c812e454","iss":"KEYCLOAK_BASE_URL_VAR/keycloak/home/auth/realms/master","startedAt":"1734058411","response_type":"code","level-of-authentication":"-1","code_challenge_method":"S256","nonce":"2bd10aba-2738-40ea-a496-83626acbe554","response_mode":"query","scope":"openid","userSessionStartedAt":"1734058411","redirect_uri":"KEYCLOAK_BASE_URL_VAR/keycloak/home/auth/admin/master/console/","state":"59ee77c3-238a-436b-862e-32f45d6c0e69","code_challenge":"NwKxGLyLQklma6ugn6NehlqNDg_rwrJIvkiiJNzW7HM"}}', 'local', 'local', 1);

-- ----------------------------
-- Table structure for offline_user_session
-- ----------------------------
DROP TABLE IF EXISTS "public"."offline_user_session";
CREATE TABLE "public"."offline_user_session" (
  "user_session_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "created_on" int4 NOT NULL,
  "offline_flag" varchar(4) COLLATE "pg_catalog"."default" NOT NULL,
  "data" text COLLATE "pg_catalog"."default",
  "last_session_refresh" int4 NOT NULL DEFAULT 0,
  "broker_session_id" varchar(1024) COLLATE "pg_catalog"."default",
  "version" int4 DEFAULT 0
)
;

-- ----------------------------
-- Records of offline_user_session
-- ----------------------------
INSERT INTO "public"."offline_user_session" VALUES ('ddfa9a6e-026a-4960-bbd5-32b08a6bf2c6', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 1734058411, '0', '{"ipAddress":"172.19.0.18","authMethod":"openid-connect","rememberMe":false,"started":0,"notes":{"KC_DEVICE_NOTE":"eyJpcEFkZHJlc3MiOiIxNzIuMTkuMC4xOCIsIm9zIjoiV2luZG93cyIsIm9zVmVyc2lvbiI6IjEwIiwiYnJvd3NlciI6IkNocm9tZS8xMzEuMC4wIiwiZGV2aWNlIjoiT3RoZXIiLCJsYXN0QWNjZXNzIjowLCJtb2JpbGUiOmZhbHNlfQ==","AUTH_TIME":"1734058411","authenticators-completed":"{\"c2eb017d-e3aa-4e5d-b962-f955d3a83585\":1734058411}"},"state":"LOGGED_IN"}', 1734058412, NULL, 1);

-- ----------------------------
-- Table structure for org
-- ----------------------------
DROP TABLE IF EXISTS "public"."org";
CREATE TABLE "public"."org" (
  "id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "enabled" bool NOT NULL,
  "realm_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "group_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "description" varchar(4000) COLLATE "pg_catalog"."default",
  "alias" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "redirect_url" varchar(2048) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of org
-- ----------------------------

-- ----------------------------
-- Table structure for org_domain
-- ----------------------------
DROP TABLE IF EXISTS "public"."org_domain";
CREATE TABLE "public"."org_domain" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "verified" bool NOT NULL,
  "org_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of org_domain
-- ----------------------------

-- ----------------------------
-- Table structure for policy_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."policy_config";
CREATE TABLE "public"."policy_config" (
  "policy_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of policy_config
-- ----------------------------
INSERT INTO "public"."policy_config" VALUES ('edfc1154-e705-44d5-b0dd-9bc2521bb603', 'code', '// by default, grants any permission associated with this policy
$evaluation.grant();
');
INSERT INTO "public"."policy_config" VALUES ('e93d7769-c6bd-4916-a47d-bfb5ca7720a1', 'defaultResourceType', 'urn:supos:resources:default');
INSERT INTO "public"."policy_config" VALUES ('548f1a2e-ec47-4862-8195-49540ca2ad3b', 'fetchRoles', 'false');
INSERT INTO "public"."policy_config" VALUES ('69e0cbbc-8c1b-4ca0-aa8a-1baa48c3f766', 'fetchRoles', 'false');
INSERT INTO "public"."policy_config" VALUES ('69e0cbbc-8c1b-4ca0-aa8a-1baa48c3f766', 'roles', '[{"id":"00e0b0d2-ba36-4927-ac5d-15c4548389f2","required":false}]');
INSERT INTO "public"."policy_config" VALUES ('b816debf-bfbd-4096-82da-4df49f07047b', 'fetchRoles', 'false');
INSERT INTO "public"."policy_config" VALUES ('b816debf-bfbd-4096-82da-4df49f07047b', 'roles', '[{"id":"2152d19d-e4f9-488d-8509-e49cf239596a","required":false}]');
INSERT INTO "public"."policy_config" VALUES ('548f1a2e-ec47-4862-8195-49540ca2ad3b', 'roles', '[{"id":"7ca9f922-0d35-44cf-8747-8dcfd5e66f8e","required":false}]');
INSERT INTO "public"."policy_config" VALUES ('61b4bf1a-f4bb-43a0-b91f-bbb37b1ab203', 'fetchRoles', 'false');
INSERT INTO "public"."policy_config" VALUES ('61b4bf1a-f4bb-43a0-b91f-bbb37b1ab203', 'roles', '[{"id":"a22ce15f-7bef-4e2e-9909-78f51b91c799","required":false}]');
INSERT INTO "public"."policy_config" VALUES ('97ad4b74-cde2-45ff-97d4-b411ac0a7153', 'fetchRoles', 'false');
INSERT INTO "public"."policy_config" VALUES ('97ad4b74-cde2-45ff-97d4-b411ac0a7153', 'roles', '[{"id":"71dd6dc2-6b12-4273-9ec0-b44b86e5b500","required":false}]');

-- ----------------------------
-- Table structure for protocol_mapper
-- ----------------------------
DROP TABLE IF EXISTS "public"."protocol_mapper";
CREATE TABLE "public"."protocol_mapper" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "protocol" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "protocol_mapper_name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "client_id" varchar(36) COLLATE "pg_catalog"."default",
  "client_scope_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of protocol_mapper
-- ----------------------------
INSERT INTO "public"."protocol_mapper" VALUES ('67d419c1-3096-4b02-b2f8-8bf0a91e7ed3', 'audience resolve', 'openid-connect', 'oidc-audience-resolve-mapper', 'fd58dd90-f09d-4bd3-b7a5-e2b81440b804', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'locale', 'openid-connect', 'oidc-usermodel-attribute-mapper', '5d654a79-6353-4eba-8f64-a0a7c812e454', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('3beb5d46-d035-43d5-a249-a4662d0a1c09', 'role list', 'saml', 'saml-role-list-mapper', NULL, 'e760b92c-cc89-486a-893d-9c0d5f792170');
INSERT INTO "public"."protocol_mapper" VALUES ('ec9d5982-79dc-4ecd-9486-a6dfd8accbad', 'organization', 'saml', 'saml-organization-membership-mapper', NULL, 'fab7a1a9-abaa-4598-8a28-e426140649ee');
INSERT INTO "public"."protocol_mapper" VALUES ('59f96f6b-330e-46f6-8ff3-cb07cdb40393', 'full name', 'openid-connect', 'oidc-full-name-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'family name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'given name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'middle name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'nickname', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'username', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'profile', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'picture', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'website', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'gender', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'birthdate', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'zoneinfo', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'locale', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'updated at', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '37b9772b-4a05-4b69-8f26-9c1ef1062650');
INSERT INTO "public"."protocol_mapper" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'email', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '955bf38a-c128-41db-b0b5-780cb1b6376d');
INSERT INTO "public"."protocol_mapper" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'email verified', 'openid-connect', 'oidc-usermodel-property-mapper', NULL, '955bf38a-c128-41db-b0b5-780cb1b6376d');
INSERT INTO "public"."protocol_mapper" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'address', 'openid-connect', 'oidc-address-mapper', NULL, 'f5e3b877-9dd6-40a5-afa3-3596fb59bd0a');
INSERT INTO "public"."protocol_mapper" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'phone number', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '0c85b1c7-6314-4a0b-b5fb-b76308dbab56');
INSERT INTO "public"."protocol_mapper" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'phone number verified', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '0c85b1c7-6314-4a0b-b5fb-b76308dbab56');
INSERT INTO "public"."protocol_mapper" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'realm roles', 'openid-connect', 'oidc-usermodel-realm-role-mapper', NULL, '0127c5fb-e845-40dc-b964-70639f5105d9');
INSERT INTO "public"."protocol_mapper" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'client roles', 'openid-connect', 'oidc-usermodel-client-role-mapper', NULL, '0127c5fb-e845-40dc-b964-70639f5105d9');
INSERT INTO "public"."protocol_mapper" VALUES ('b7db0093-918b-446e-ae80-47d22d753a59', 'audience resolve', 'openid-connect', 'oidc-audience-resolve-mapper', NULL, '0127c5fb-e845-40dc-b964-70639f5105d9');
INSERT INTO "public"."protocol_mapper" VALUES ('82ef02b4-1aa1-48f5-9945-a1b0c7716832', 'allowed web origins', 'openid-connect', 'oidc-allowed-origins-mapper', NULL, '1da7a744-af7f-4da8-86d6-c4a8f49bed77');
INSERT INTO "public"."protocol_mapper" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'upn', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19990782-99f3-4f22-a383-0c4832e4f781');
INSERT INTO "public"."protocol_mapper" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'groups', 'openid-connect', 'oidc-usermodel-realm-role-mapper', NULL, '19990782-99f3-4f22-a383-0c4832e4f781');
INSERT INTO "public"."protocol_mapper" VALUES ('a374b668-210d-43d4-9ff8-c53b60f7468e', 'acr loa level', 'openid-connect', 'oidc-acr-mapper', NULL, 'f6a95e92-945c-45fa-88ba-d4482ed7f9e0');
INSERT INTO "public"."protocol_mapper" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'auth_time', 'openid-connect', 'oidc-usersessionmodel-note-mapper', NULL, '8b71be46-e97e-4fb6-8044-ab9e91be71b2');
INSERT INTO "public"."protocol_mapper" VALUES ('663127cd-c32c-4399-83ba-43e8bec95648', 'sub', 'openid-connect', 'oidc-sub-mapper', NULL, '8b71be46-e97e-4fb6-8044-ab9e91be71b2');
INSERT INTO "public"."protocol_mapper" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'organization', 'openid-connect', 'oidc-organization-membership-mapper', NULL, '8a1ce846-12c2-43ab-b220-93dcf015f2b6');
INSERT INTO "public"."protocol_mapper" VALUES ('bd510639-3ef5-412d-8c9d-109b0ad19a73', 'audience resolve', 'openid-connect', 'oidc-audience-resolve-mapper', 'bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('5f70aff3-9a6d-4a0e-ae8d-a1dd2b98851b', 'role list', 'saml', 'saml-role-list-mapper', NULL, '0dc62ecf-9c4c-40fb-ba9a-4725abf1d3ff');
INSERT INTO "public"."protocol_mapper" VALUES ('d6e64ad4-9f63-418b-bcce-161799276225', 'organization', 'saml', 'saml-organization-membership-mapper', NULL, 'f7db029c-7313-47c1-a6e8-56050682927f');
INSERT INTO "public"."protocol_mapper" VALUES ('c30061c9-f215-4f18-b03d-270f76b6ec95', 'full name', 'openid-connect', 'oidc-full-name-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'family name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'given name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'middle name', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'nickname', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'username', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'profile', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'picture', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'website', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'gender', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'birthdate', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'zoneinfo', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'locale', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'updated at', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '19b9d4dd-3607-4e3a-838e-b156630fe78e');
INSERT INTO "public"."protocol_mapper" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'email', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, 'aab92fd1-d7b8-456c-aa9f-19c6c782260c');
INSERT INTO "public"."protocol_mapper" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'email verified', 'openid-connect', 'oidc-usermodel-property-mapper', NULL, 'aab92fd1-d7b8-456c-aa9f-19c6c782260c');
INSERT INTO "public"."protocol_mapper" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'address', 'openid-connect', 'oidc-address-mapper', NULL, 'cca2f7fe-1d61-468e-a9df-83d25f108dc2');
INSERT INTO "public"."protocol_mapper" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'realm roles', 'openid-connect', 'oidc-usermodel-realm-role-mapper', NULL, 'ad9286f6-2377-4db7-872b-5edcbef2017a');
INSERT INTO "public"."protocol_mapper" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'client roles', 'openid-connect', 'oidc-usermodel-client-role-mapper', NULL, 'ad9286f6-2377-4db7-872b-5edcbef2017a');
INSERT INTO "public"."protocol_mapper" VALUES ('5f7337ac-2599-4806-9858-f18fec82240a', 'audience resolve', 'openid-connect', 'oidc-audience-resolve-mapper', NULL, 'ad9286f6-2377-4db7-872b-5edcbef2017a');
INSERT INTO "public"."protocol_mapper" VALUES ('c6ce4761-4df1-40a2-ab93-d8a7d5d8eca1', 'allowed web origins', 'openid-connect', 'oidc-allowed-origins-mapper', NULL, '147d0a04-66fc-49db-a1c4-fa233eb47825');
INSERT INTO "public"."protocol_mapper" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'upn', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, 'fcc54556-ec96-4011-a89e-7c1d0ea2e714');
INSERT INTO "public"."protocol_mapper" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'groups', 'openid-connect', 'oidc-usermodel-realm-role-mapper', NULL, 'fcc54556-ec96-4011-a89e-7c1d0ea2e714');
INSERT INTO "public"."protocol_mapper" VALUES ('298dcbcc-b13f-4996-be22-64cb8f837d63', 'acr loa level', 'openid-connect', 'oidc-acr-mapper', NULL, '80edc885-da5f-472c-90cc-d8b0e6d1f011');
INSERT INTO "public"."protocol_mapper" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'auth_time', 'openid-connect', 'oidc-usersessionmodel-note-mapper', NULL, '9475e044-78d6-41ac-88a8-0cc0cedf5875');
INSERT INTO "public"."protocol_mapper" VALUES ('294c3d53-b4a1-42ea-a178-935eb413c225', 'sub', 'openid-connect', 'oidc-sub-mapper', NULL, '9475e044-78d6-41ac-88a8-0cc0cedf5875');
INSERT INTO "public"."protocol_mapper" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'organization', 'openid-connect', 'oidc-organization-membership-mapper', NULL, 'e5d6dd73-37ab-4864-abd8-b473bc110772');
INSERT INTO "public"."protocol_mapper" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'locale', 'openid-connect', 'oidc-usermodel-attribute-mapper', '200c8e2f-4b1e-46e0-b1f6-92b060f2717c', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'Client ID', 'openid-connect', 'oidc-usersessionmodel-note-mapper', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'Client Host', 'openid-connect', 'oidc-usersessionmodel-note-mapper', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'Client IP Address', 'openid-connect', 'oidc-usersessionmodel-note-mapper', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."protocol_mapper" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'firstTimeLogin', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '2c26c6cb-b18b-4fd9-bbde-38d81cfaa038');
INSERT INTO "public"."protocol_mapper" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'tipsEnable', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '4e44f85d-bb73-4eb9-af2a-c1a641792a94');
INSERT INTO "public"."protocol_mapper" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'homePage', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '51ec41ba-ea8b-4359-80a3-e3de154ee389');
INSERT INTO "public"."protocol_mapper" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'phone', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '7ee32b54-6b11-4f84-ae7a-bec36a6fd1ec');
INSERT INTO "public"."protocol_mapper" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'source', 'openid-connect', 'oidc-usermodel-attribute-mapper', NULL, '804408e1-e065-4362-8cd1-414c9b9777b3');

-- ----------------------------
-- Table structure for protocol_mapper_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."protocol_mapper_config";
CREATE TABLE "public"."protocol_mapper_config" (
  "protocol_mapper_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of protocol_mapper_config
-- ----------------------------
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'locale', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'locale', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d7ea6460-d0c7-4871-ac57-2afcec886822', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3beb5d46-d035-43d5-a249-a4662d0a1c09', 'false', 'single');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3beb5d46-d035-43d5-a249-a4662d0a1c09', 'Basic', 'attribute.nameformat');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3beb5d46-d035-43d5-a249-a4662d0a1c09', 'Role', 'attribute.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'website', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'website', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('128d0a5b-de05-45af-99eb-5d2e7082c76b', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'birthdate', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'birthdate', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14127a9e-7543-4067-a63c-8dc5bf51a7ee', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'profile', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'profile', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('19c14482-5a27-41c2-adbc-f95dc3c70059', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'firstName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'given_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2487a374-4e9b-4f04-a712-046946c74be9', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'middleName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'middle_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('26190edb-723e-41e4-83e4-53dcc06e3bcc', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'picture', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'picture', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2bde9f0f-0480-4a84-9207-e42ce2999621', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'nickname', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'nickname', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3179b6b6-7221-4bdd-b5e7-517e414703a4', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'updatedAt', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'updated_at', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('36c88403-b8b7-4595-8fbd-1b78aa688a58', 'long', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('59f96f6b-330e-46f6-8ff3-cb07cdb40393', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('59f96f6b-330e-46f6-8ff3-cb07cdb40393', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('59f96f6b-330e-46f6-8ff3-cb07cdb40393', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('59f96f6b-330e-46f6-8ff3-cb07cdb40393', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'username', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'preferred_username', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('60b2ec87-3191-44af-a39d-69296ef2950d', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'zoneinfo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'zoneinfo', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('7b8a31ab-045e-4480-b2d0-741ca3ce40e7', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'gender', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'gender', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9d0b1059-57f2-4fea-8d5c-cf6aab39897a', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'lastName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'family_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b42648ea-7a60-4386-aa8e-edc317493697', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'locale', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'locale', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b460fcae-1c06-4743-8d44-a9d67ba46b9d', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'email', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'email', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2c113199-f1e9-4ba8-abdd-adf83aee3ab9', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'emailVerified', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'email_verified', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('f0f90c95-b582-47ec-9654-cb673672d643', 'boolean', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'formatted', 'user.attribute.formatted');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'country', 'user.attribute.country');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'postal_code', 'user.attribute.postal_code');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'street', 'user.attribute.street');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'region', 'user.attribute.region');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4f8f8015-c12e-4252-8559-cc0fcc6e39c9', 'locality', 'user.attribute.locality');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'phoneNumberVerified', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'phone_number_verified', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('08c04c75-4777-4cfd-a32a-9a8792df88b4', 'boolean', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'phoneNumber', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'phone_number', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('53e3bf71-78b3-42b7-9abb-02906d686cb9', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'realm_access.roles', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('1a3f0413-9e17-4039-8feb-aabb57f2116a', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'resource_access.${client_id}.roles', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('373e6ca1-a58d-4471-b7d6-962a412c2b3b', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b7db0093-918b-446e-ae80-47d22d753a59', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b7db0093-918b-446e-ae80-47d22d753a59', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('82ef02b4-1aa1-48f5-9945-a1b0c7716832', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('82ef02b4-1aa1-48f5-9945-a1b0c7716832', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'groups', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('379ba4e7-11bb-48ec-ac87-e6c81dc17cdd', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'username', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'upn', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9c5b122a-4b8f-4e4c-9d3d-e815789068bd', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a374b668-210d-43d4-9ff8-c53b60f7468e', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a374b668-210d-43d4-9ff8-c53b60f7468e', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a374b668-210d-43d4-9ff8-c53b60f7468e', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('663127cd-c32c-4399-83ba-43e8bec95648', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('663127cd-c32c-4399-83ba-43e8bec95648', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'AUTH_TIME', 'user.session.note');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'auth_time', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('dfd0bb25-7048-4444-9c99-120aed63a6c2', 'long', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'organization', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('10193ecf-a937-45e9-b3f5-b53b48e75509', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5f70aff3-9a6d-4a0e-ae8d-a1dd2b98851b', 'false', 'single');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5f70aff3-9a6d-4a0e-ae8d-a1dd2b98851b', 'Basic', 'attribute.nameformat');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5f70aff3-9a6d-4a0e-ae8d-a1dd2b98851b', 'Role', 'attribute.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'nickname', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'nickname', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('123844f4-8265-4549-bcca-608dd207ed92', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'lastName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'family_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('15936bcd-48f4-4e74-b32d-d657fd17ffad', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'picture', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'picture', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('185c878b-35b2-49a1-a39d-46ecb9b527e4', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'firstName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'given_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('2001144f-eefe-4229-a230-3de126e0f3aa', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'updatedAt', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'updated_at', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('3dec4767-8ae4-44b5-a8ad-1a8f2c0fbd95', 'long', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'zoneinfo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'zoneinfo', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('85b006d4-1501-42b6-b2e9-8037a6b60bdb', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'locale', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'locale', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8ec5791f-891f-4600-a46a-203048aa6ab8', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'gender', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'gender', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a2f1d6c1-5928-4b6b-bc03-2245c763f20e', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'profile', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'profile', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('b1895a89-6529-4bb9-bfc5-9d2e2ab69a84', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'middleName', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'middle_name', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bd3bdca2-9d9f-4514-903c-87efbb36a9cd', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c30061c9-f215-4f18-b03d-270f76b6ec95', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c30061c9-f215-4f18-b03d-270f76b6ec95', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c30061c9-f215-4f18-b03d-270f76b6ec95', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c30061c9-f215-4f18-b03d-270f76b6ec95', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'username', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'preferred_username', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c33a8b69-545e-400a-9af0-d1bfbbd022b1', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'website', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'website', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('ddb0669e-c158-47bd-9d16-aacb1c1c5725', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'birthdate', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'birthdate', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('e82b6fcd-0eff-476f-8ca2-81d1155726ed', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'email', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'email', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('09f4920e-02f1-4517-b696-330516722f74', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'emailVerified', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'email_verified', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4d5a216c-451b-42fa-906d-ea9c2af374a2', 'boolean', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'formatted', 'user.attribute.formatted');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'country', 'user.attribute.country');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'postal_code', 'user.attribute.postal_code');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'street', 'user.attribute.street');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'region', 'user.attribute.region');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('d2ad9f0c-7a84-4488-8ac8-20332f3a7a1e', 'locality', 'user.attribute.locality');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'realm_access.roles', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('14ee99a6-926a-480c-a6eb-fbb84f59f0db', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'resource_access.${client_id}.roles', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('187bdc08-0f19-47f9-9805-3f96fe9c28d0', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5f7337ac-2599-4806-9858-f18fec82240a', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5f7337ac-2599-4806-9858-f18fec82240a', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c6ce4761-4df1-40a2-ab93-d8a7d5d8eca1', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('c6ce4761-4df1-40a2-ab93-d8a7d5d8eca1', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'username', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'upn', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('4ecee324-18f0-4567-af0c-d299efdaad29', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'foo', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'groups', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a857fd4e-e747-403f-bb3b-b3da5b06223a', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('298dcbcc-b13f-4996-be22-64cb8f837d63', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('298dcbcc-b13f-4996-be22-64cb8f837d63', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('298dcbcc-b13f-4996-be22-64cb8f837d63', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('294c3d53-b4a1-42ea-a178-935eb413c225', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('294c3d53-b4a1-42ea-a178-935eb413c225', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'AUTH_TIME', 'user.session.note');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'auth_time', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8d296dd2-5bbb-41b4-9fa5-e02fe7bfeb62', 'long', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'true', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'organization', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('9298adf6-8117-4512-8d21-2d00271cdc17', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'locale', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'locale', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('bde7b407-9591-43fc-baaf-906e5d9cec64', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'client_id', 'user.session.note');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'client_id', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('258174bf-3f48-46d6-ae4e-2daeb9806cac', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'clientAddress', 'user.session.note');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'clientAddress', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('8bc84fef-26a9-4bf6-a94d-978d4d8bf4e3', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'clientHost', 'user.session.note');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'clientHost', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('a5d358bd-92c0-44e5-85fa-5c8a6e354675', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'firstTimeLogin', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'false', 'lightweight.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'firstTimeLogin', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'false', 'aggregate.attrs');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'false', 'multivalued');
INSERT INTO "public"."protocol_mapper_config" VALUES ('79e141a5-93e9-48c6-b1e2-2efd42aa62e8', 'int', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'tipsEnable', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'false', 'lightweight.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'tipsEnable', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0dee1f53-4bb9-49df-96a9-d22ece81ca32', 'int', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'homePage', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'false', 'lightweight.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'homePage', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('99cc17f5-aa0a-452e-873f-d8f669fe32c8', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'phone', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'false', 'lightweight.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'phone', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('0e4ae11e-8be7-4c4f-a742-7c725b007e4d', 'String', 'jsonType.label');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'true', 'introspection.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'true', 'userinfo.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'source', 'user.attribute');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'true', 'id.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'false', 'lightweight.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'true', 'access.token.claim');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'source', 'claim.name');
INSERT INTO "public"."protocol_mapper_config" VALUES ('5ae3dfa7-bd5a-45f9-945f-f9864c1edecb', 'String', 'jsonType.label');


-- ----------------------------
-- Table structure for realm
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm";
CREATE TABLE "public"."realm" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "access_code_lifespan" int4,
  "user_action_lifespan" int4,
  "access_token_lifespan" int4,
  "account_theme" varchar(255) COLLATE "pg_catalog"."default",
  "admin_theme" varchar(255) COLLATE "pg_catalog"."default",
  "email_theme" varchar(255) COLLATE "pg_catalog"."default",
  "enabled" bool NOT NULL DEFAULT false,
  "events_enabled" bool NOT NULL DEFAULT false,
  "events_expiration" int8,
  "login_theme" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "not_before" int4,
  "password_policy" varchar(2550) COLLATE "pg_catalog"."default",
  "registration_allowed" bool NOT NULL DEFAULT false,
  "remember_me" bool NOT NULL DEFAULT false,
  "reset_password_allowed" bool NOT NULL DEFAULT false,
  "social" bool NOT NULL DEFAULT false,
  "ssl_required" varchar(255) COLLATE "pg_catalog"."default",
  "sso_idle_timeout" int4,
  "sso_max_lifespan" int4,
  "update_profile_on_soc_login" bool NOT NULL DEFAULT false,
  "verify_email" bool NOT NULL DEFAULT false,
  "master_admin_client" varchar(36) COLLATE "pg_catalog"."default",
  "login_lifespan" int4,
  "internationalization_enabled" bool NOT NULL DEFAULT false,
  "default_locale" varchar(255) COLLATE "pg_catalog"."default",
  "reg_email_as_username" bool NOT NULL DEFAULT false,
  "admin_events_enabled" bool NOT NULL DEFAULT false,
  "admin_events_details_enabled" bool NOT NULL DEFAULT false,
  "edit_username_allowed" bool NOT NULL DEFAULT false,
  "otp_policy_counter" int4 DEFAULT 0,
  "otp_policy_window" int4 DEFAULT 1,
  "otp_policy_period" int4 DEFAULT 30,
  "otp_policy_digits" int4 DEFAULT 6,
  "otp_policy_alg" varchar(36) COLLATE "pg_catalog"."default" DEFAULT 'HmacSHA1'::character varying,
  "otp_policy_type" varchar(36) COLLATE "pg_catalog"."default" DEFAULT 'totp'::character varying,
  "browser_flow" varchar(36) COLLATE "pg_catalog"."default",
  "registration_flow" varchar(36) COLLATE "pg_catalog"."default",
  "direct_grant_flow" varchar(36) COLLATE "pg_catalog"."default",
  "reset_credentials_flow" varchar(36) COLLATE "pg_catalog"."default",
  "client_auth_flow" varchar(36) COLLATE "pg_catalog"."default",
  "offline_session_idle_timeout" int4 DEFAULT 0,
  "revoke_refresh_token" bool NOT NULL DEFAULT false,
  "access_token_life_implicit" int4 DEFAULT 0,
  "login_with_email_allowed" bool NOT NULL DEFAULT true,
  "duplicate_emails_allowed" bool NOT NULL DEFAULT false,
  "docker_auth_flow" varchar(36) COLLATE "pg_catalog"."default",
  "refresh_token_max_reuse" int4 DEFAULT 0,
  "allow_user_managed_access" bool NOT NULL DEFAULT false,
  "sso_max_lifespan_remember_me" int4 NOT NULL DEFAULT 0,
  "sso_idle_timeout_remember_me" int4 NOT NULL DEFAULT 0,
  "default_role" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of realm
-- ----------------------------
INSERT INTO "public"."realm" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 1800, 300, 108000, 'keycloak.v3', 'keycloak.v2', 'keycloak', 't', 'f', 0, 'keycloak.v2', 'master', 0, NULL, 'f', 'f', 'f', 'f', 'EXTERNAL', 1800, 36000, 'f', 'f', 'b478982d-7f85-498c-8b83-2903d6c1116a', 1800, 't', 'LANGUAGE_VAR', 'f', 'f', 'f', 'f', 0, 1, 30, 6, 'HmacSHA1', 'totp', 'a6b294ac-ffaa-4f9e-accd-9f6d38c2f634', '6e0df28a-8860-4037-b9ca-0efcae55b8b4', 'd6da9d19-cb71-4841-9cfa-90a111d1b292', '4759605b-ec2d-48ad-9754-49e4be8ba93a', '7e12f5b6-3b89-41fb-b40b-2d1731f02c14', 2592000, 'f', 1800, 't', 'f', '3d8206dd-13aa-46b4-a0fc-444e5b9af4a6', 0, 'f', 0, 0, '78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc');
INSERT INTO "public"."realm" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 7200, 43200, 43200, 'keycloak.v3', 'keycloak.v2', 'keycloak', 't', 'f', 60, 'wenhao', 'supos', 0, NULL, 'f', 'f', 'f', 'f', 'EXTERNAL', 43200, 43200, 'f', 'f', 'c7e2b1e8-0574-4441-b337-caa811fc3a75', 86400, 't', 'LANGUAGE_VAR', 'f', 'f', 'f', 'f', 0, 1, 30, 6, 'HmacSHA1', 'totp', 'e007fedd-2171-44f9-826f-fa91f76ffe20', '88e45fd1-dc20-4df7-a4f7-47325ceb7e02', '13eaac88-bd3c-4309-ac4c-e7aef4b566da', '7f6ac3fc-86b5-48dc-9984-16b5b26b449b', '7b041d92-f8f9-46ac-81a0-a712cb0dc122', 2592000, 'f', 7200, 'f', 't', '67631c78-793f-4605-98e3-be7756eba483', 0, 'f', 43200, 43200, 'b51711f1-2430-4a24-8493-ab1d9b0dde6f');

-- ----------------------------
-- Table structure for realm_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_attribute";
CREATE TABLE "public"."realm_attribute" (
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of realm_attribute
-- ----------------------------
INSERT INTO "public"."realm_attribute" VALUES ('parRequestUriLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7200');
INSERT INTO "public"."realm_attribute" VALUES ('oauth2DeviceCodeLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '7200');
INSERT INTO "public"."realm_attribute" VALUES ('bruteForceProtected', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('permanentLockout', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('maxTemporaryLockouts', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('maxFailureWaitSeconds', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '900');
INSERT INTO "public"."realm_attribute" VALUES ('minimumQuickLoginWaitSeconds', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '60');
INSERT INTO "public"."realm_attribute" VALUES ('waitIncrementSeconds', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '60');
INSERT INTO "public"."realm_attribute" VALUES ('quickLoginCheckMilliSeconds', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '1000');
INSERT INTO "public"."realm_attribute" VALUES ('maxDeltaTimeSeconds', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('failureFactor', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '30');
INSERT INTO "public"."realm_attribute" VALUES ('realmReusableOtpCode', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('firstBrokerLoginFlowId', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3e2d7701-9174-4e6a-93fc-e63781247499');
INSERT INTO "public"."realm_attribute" VALUES ('displayName', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'Keycloak');
INSERT INTO "public"."realm_attribute" VALUES ('displayNameHtml', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '<div class="kc-logo-text"><span>Keycloak</span></div>');
INSERT INTO "public"."realm_attribute" VALUES ('defaultSignatureAlgorithm', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'RS256');
INSERT INTO "public"."realm_attribute" VALUES ('offlineSessionMaxLifespanEnabled', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('offlineSessionMaxLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '5184000');
INSERT INTO "public"."realm_attribute" VALUES ('bruteForceProtected', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('permanentLockout', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('maxTemporaryLockouts', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0');
INSERT INTO "public"."realm_attribute" VALUES ('maxFailureWaitSeconds', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '900');
INSERT INTO "public"."realm_attribute" VALUES ('minimumQuickLoginWaitSeconds', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '60');
INSERT INTO "public"."realm_attribute" VALUES ('waitIncrementSeconds', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '60');
INSERT INTO "public"."realm_attribute" VALUES ('quickLoginCheckMilliSeconds', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1000');
INSERT INTO "public"."realm_attribute" VALUES ('maxDeltaTimeSeconds', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('failureFactor', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '30');
INSERT INTO "public"."realm_attribute" VALUES ('realmReusableOtpCode', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('defaultSignatureAlgorithm', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'RS256');
INSERT INTO "public"."realm_attribute" VALUES ('offlineSessionMaxLifespanEnabled', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('offlineSessionMaxLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5184000');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByAdminLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '300');
INSERT INTO "public"."realm_attribute" VALUES ('oauth2DevicePollingInterval', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpEntityName', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'keycloak');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicySignatureAlgorithms', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'ES256,RS256');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpId', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAttestationConveyancePreference', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAuthenticatorAttachment', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRequireResidentKey', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyUserVerificationRequirement', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyCreateTimeout', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAvoidSameAuthenticatorRegister', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpEntityNamePasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'keycloak');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicySignatureAlgorithmsPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'ES256,RS256');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpIdPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAttestationConveyancePreferencePasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAuthenticatorAttachmentPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRequireResidentKeyPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyUserVerificationRequirementPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyCreateTimeoutPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAvoidSameAuthenticatorRegisterPasswordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('cibaBackchannelTokenDeliveryMode', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'poll');
INSERT INTO "public"."realm_attribute" VALUES ('cibaExpiresIn', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '120');
INSERT INTO "public"."realm_attribute" VALUES ('cibaInterval', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '5');
INSERT INTO "public"."realm_attribute" VALUES ('cibaAuthRequestedUserHint', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'login_hint');
INSERT INTO "public"."realm_attribute" VALUES ('firstBrokerLoginFlowId', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '545cbde1-6116-461f-a031-ff051faf7c21');
INSERT INTO "public"."realm_attribute" VALUES ('organizationsEnabled', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('shortVerificationUri', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.verify-email', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.idp-verify-account-via-email', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.reset-credentials', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.execute-actions', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('clientOfflineSessionIdleTimeout', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0');
INSERT INTO "public"."realm_attribute" VALUES ('clientOfflineSessionMaxLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '0');
INSERT INTO "public"."realm_attribute" VALUES ('client-policies.profiles', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '{"profiles":[]}');
INSERT INTO "public"."realm_attribute" VALUES ('client-policies.policies', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '{"policies":[]}');
INSERT INTO "public"."realm_attribute" VALUES ('bruteForceStrategy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'MULTIPLE');
INSERT INTO "public"."realm_attribute" VALUES ('cibaBackchannelTokenDeliveryMode', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'poll');
INSERT INTO "public"."realm_attribute" VALUES ('cibaExpiresIn', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '120');
INSERT INTO "public"."realm_attribute" VALUES ('cibaAuthRequestedUserHint', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'login_hint');
INSERT INTO "public"."realm_attribute" VALUES ('cibaInterval', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '5');
INSERT INTO "public"."realm_attribute" VALUES ('bruteForceStrategy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'MULTIPLE');
INSERT INTO "public"."realm_attribute" VALUES ('organizationsEnabled', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByAdminLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '300');
INSERT INTO "public"."realm_attribute" VALUES ('oauth2DeviceCodeLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '600');
INSERT INTO "public"."realm_attribute" VALUES ('oauth2DevicePollingInterval', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '5');
INSERT INTO "public"."realm_attribute" VALUES ('clientSessionIdleTimeout', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('clientSessionMaxLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('clientOfflineSessionIdleTimeout', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('clientOfflineSessionMaxLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpEntityName', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'keycloak');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicySignatureAlgorithms', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'ES256,RS256');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpId', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAttestationConveyancePreference', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAuthenticatorAttachment', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRequireResidentKey', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyUserVerificationRequirement', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyCreateTimeout', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAvoidSameAuthenticatorRegister', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpEntityNamePasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'keycloak');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicySignatureAlgorithmsPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'ES256,RS256');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRpIdPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAttestationConveyancePreferencePasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAuthenticatorAttachmentPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyRequireResidentKeyPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyUserVerificationRequirementPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'not specified');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyCreateTimeoutPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '0');
INSERT INTO "public"."realm_attribute" VALUES ('webAuthnPolicyAvoidSameAuthenticatorRegisterPasswordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'false');
INSERT INTO "public"."realm_attribute" VALUES ('client-policies.profiles', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '{"profiles":[]}');
INSERT INTO "public"."realm_attribute" VALUES ('client-policies.policies', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '{"policies":[]}');
INSERT INTO "public"."realm_attribute" VALUES ('shortVerificationUri', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.verify-email', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.idp-verify-account-via-email', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.reset-credentials', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('actionTokenGeneratedByUserLifespan.execute-actions', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('parRequestUriLifespan', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '3600');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.contentSecurityPolicyReportOnly', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xContentTypeOptions', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'nosniff');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.referrerPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'no-referrer');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xRobotsTag', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'none');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xFrameOptions', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'SAMEORIGIN');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.contentSecurityPolicy', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'frame-src ''self''; frame-ancestors ''self''; object-src ''none'';');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xXSSProtection', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', '1; mode=block');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.strictTransportSecurity', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'max-age=31536000; includeSubDomains');
INSERT INTO "public"."realm_attribute" VALUES ('clientSessionMaxLifespan', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('clientSessionIdleTimeout', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '43200');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.contentSecurityPolicyReportOnly', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xContentTypeOptions', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'nosniff');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.referrerPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'no-referrer');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xRobotsTag', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'none');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xFrameOptions', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SAMEORIGIN');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.contentSecurityPolicy', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'frame-src ''self''; frame-ancestors ''self''; object-src ''none'';');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.xXSSProtection', '8920b375-d705-4d30-8a71-52d9c14ec4ba', '1; mode=block');
INSERT INTO "public"."realm_attribute" VALUES ('_browser_header.strictTransportSecurity', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'max-age=31536000; includeSubDomains');

-- ----------------------------
-- Table structure for realm_default_groups
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_default_groups";
CREATE TABLE "public"."realm_default_groups" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "group_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_default_groups
-- ----------------------------

-- ----------------------------
-- Table structure for realm_enabled_event_types
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_enabled_event_types";
CREATE TABLE "public"."realm_enabled_event_types" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_enabled_event_types
-- ----------------------------
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_CONSENT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_RESET_PASSWORD');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'GRANT_CONSENT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'VERIFY_PROFILE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_TOTP');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_TOTP');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REVOKE_GRANT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'LOGIN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_LOGIN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'RESET_PASSWORD_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_CREDENTIAL');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IMPERSONATE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CODE_TO_TOKEN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CUSTOM_REQUIRED_ACTION');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_CODE_TO_TOKEN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'RESTART_AUTHENTICATION');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_PROFILE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IMPERSONATE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'LOGIN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_PASSWORD_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_VERIFY_USER_CODE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_INITIATED_ACCOUNT_LINKING');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'USER_DISABLED_BY_PERMANENT_LOCKOUT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_EXTENSION_GRANT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_CREDENTIAL_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'TOKEN_EXCHANGE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REGISTER');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'LOGOUT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'AUTHREQID_TO_TOKEN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'DELETE_ACCOUNT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_REGISTER');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_LINK_ACCOUNT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'USER_DISABLED_BY_TEMPORARY_LOCKOUT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_PASSWORD');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'DELETE_ACCOUNT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'FEDERATED_IDENTITY_LINK_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_DELETE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_FIRST_LOGIN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'VERIFY_EMAIL');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_DELETE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_LOGIN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'RESTART_AUTHENTICATION_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_FEDERATED_IDENTITY_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'EXECUTE_ACTIONS');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'TOKEN_EXCHANGE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'PERMISSION_TOKEN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'FEDERATED_IDENTITY_OVERRIDE_LINK');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_IDENTITY_PROVIDER_LINK_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_CREDENTIAL_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'EXECUTE_ACTION_TOKEN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_VERIFY_EMAIL');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_EXTENSION_GRANT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_AUTH');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'EXECUTE_ACTIONS_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_FEDERATED_IDENTITY');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_CODE_TO_TOKEN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_POST_LOGIN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_LINK_ACCOUNT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'FEDERATED_IDENTITY_OVERRIDE_LINK_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_EMAIL');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_VERIFY_USER_CODE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REGISTER_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REVOKE_GRANT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'LOGOUT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_EMAIL_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'EXECUTE_ACTION_TOKEN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_UPDATE_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_PROFILE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'AUTHREQID_TO_TOKEN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'INVITE_ORG_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'FEDERATED_IDENTITY_LINK');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_REGISTER_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'INVITE_ORG');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_VERIFY_EMAIL_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_IDENTITY_PROVIDER_LINK');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'RESET_PASSWORD');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_INITIATED_ACCOUNT_LINKING_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'OAUTH2_DEVICE_AUTH_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_CREDENTIAL');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_CONSENT');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'REMOVE_TOTP_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'VERIFY_EMAIL_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'SEND_RESET_PASSWORD_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CLIENT_UPDATE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_POST_LOGIN_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CUSTOM_REQUIRED_ACTION_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'UPDATE_TOTP_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'CODE_TO_TOKEN');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'VERIFY_PROFILE');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'GRANT_CONSENT_ERROR');
INSERT INTO "public"."realm_enabled_event_types" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'IDENTITY_PROVIDER_FIRST_LOGIN_ERROR');

-- ----------------------------
-- Table structure for realm_events_listeners
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_events_listeners";
CREATE TABLE "public"."realm_events_listeners" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_events_listeners
-- ----------------------------
INSERT INTO "public"."realm_events_listeners" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'jboss-logging');
INSERT INTO "public"."realm_events_listeners" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'jboss-logging');

-- ----------------------------
-- Table structure for realm_localizations
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_localizations";
CREATE TABLE "public"."realm_localizations" (
  "realm_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "locale" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "texts" text COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_localizations
-- ----------------------------

-- ----------------------------
-- Table structure for realm_required_credential
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_required_credential";
CREATE TABLE "public"."realm_required_credential" (
  "type" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "form_label" varchar(255) COLLATE "pg_catalog"."default",
  "input" bool NOT NULL DEFAULT false,
  "secret" bool NOT NULL DEFAULT false,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_required_credential
-- ----------------------------
INSERT INTO "public"."realm_required_credential" VALUES ('password', 'password', 't', 't', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724');
INSERT INTO "public"."realm_required_credential" VALUES ('password', 'password', 't', 't', '8920b375-d705-4d30-8a71-52d9c14ec4ba');

-- ----------------------------
-- Table structure for realm_smtp_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_smtp_config";
CREATE TABLE "public"."realm_smtp_config" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_smtp_config
-- ----------------------------
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'Supos1304Freezonex', 'password');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '', 'replyToDisplayName');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'true', 'starttls');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'true', 'auth');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '80', 'port');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'smtpdm.aliyun.com', 'host');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '', 'replyTo');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'no-reply.supos-ce@mail.supos.app', 'from');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'no-reply', 'fromDisplayName');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', '', 'envelopeFrom');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'false', 'ssl');
INSERT INTO "public"."realm_smtp_config" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'no-reply.supos-ce@mail.supos.app', 'user');

-- ----------------------------
-- Table structure for realm_supported_locales
-- ----------------------------
DROP TABLE IF EXISTS "public"."realm_supported_locales";
CREATE TABLE "public"."realm_supported_locales" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of realm_supported_locales
-- ----------------------------
INSERT INTO "public"."realm_supported_locales" VALUES ('ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'LANGUAGE_VAR');
INSERT INTO "public"."realm_supported_locales" VALUES ('8920b375-d705-4d30-8a71-52d9c14ec4ba', 'LANGUAGE_VAR');

-- ----------------------------
-- Table structure for redirect_uris
-- ----------------------------
DROP TABLE IF EXISTS "public"."redirect_uris";
CREATE TABLE "public"."redirect_uris" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of redirect_uris
-- ----------------------------
INSERT INTO "public"."redirect_uris" VALUES ('9a5a698a-2bdf-431c-893c-cea1ca8d7218', '/realms/master/account/*');
INSERT INTO "public"."redirect_uris" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '/realms/master/account/*');
INSERT INTO "public"."redirect_uris" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '/admin/master/console/*');
INSERT INTO "public"."redirect_uris" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '/realms/supos/account/*');
INSERT INTO "public"."redirect_uris" VALUES ('dc2e7749-eb5c-4249-ae0a-40abd10990a7', '/realms/supos/account/*');
INSERT INTO "public"."redirect_uris" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '/admin/supos/console/*');
INSERT INTO "public"."redirect_uris" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '/*');

-- ----------------------------
-- Table structure for required_action_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."required_action_config";
CREATE TABLE "public"."required_action_config" (
  "required_action_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" text COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of required_action_config
-- ----------------------------

-- ----------------------------
-- Table structure for required_action_provider
-- ----------------------------
DROP TABLE IF EXISTS "public"."required_action_provider";
CREATE TABLE "public"."required_action_provider" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "alias" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default",
  "enabled" bool NOT NULL DEFAULT false,
  "default_action" bool NOT NULL DEFAULT false,
  "provider_id" varchar(255) COLLATE "pg_catalog"."default",
  "priority" int4
)
;

-- ----------------------------
-- Records of required_action_provider
-- ----------------------------
INSERT INTO "public"."required_action_provider" VALUES ('af1d4135-a618-488c-b45b-8e9b3fb6125c', 'VERIFY_EMAIL', 'Verify Email', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'VERIFY_EMAIL', 50);
INSERT INTO "public"."required_action_provider" VALUES ('223b6124-9b5d-467e-b128-750a3e5e3a3b', 'UPDATE_PROFILE', 'Update Profile', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'UPDATE_PROFILE', 40);
INSERT INTO "public"."required_action_provider" VALUES ('767078e7-06d3-4dd4-bf1f-6ee9ae891142', 'CONFIGURE_TOTP', 'Configure OTP', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'CONFIGURE_TOTP', 10);
INSERT INTO "public"."required_action_provider" VALUES ('609fcac0-c787-4d26-bb08-cc72bd375477', 'UPDATE_PASSWORD', 'Update Password', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'UPDATE_PASSWORD', 30);
INSERT INTO "public"."required_action_provider" VALUES ('8ca0238b-1948-4a67-b0a5-7a113c5cf36d', 'TERMS_AND_CONDITIONS', 'Terms and Conditions', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', 'f', 'TERMS_AND_CONDITIONS', 20);
INSERT INTO "public"."required_action_provider" VALUES ('c6f6ae82-f2c7-463e-be1b-69e470d30a10', 'delete_account', 'Delete Account', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'f', 'f', 'delete_account', 60);
INSERT INTO "public"."required_action_provider" VALUES ('e6f4554b-42e3-4605-bd43-5c02a22a61b9', 'delete_credential', 'Delete Credential', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'delete_credential', 100);
INSERT INTO "public"."required_action_provider" VALUES ('2fe83082-69be-4862-817a-24936208b83e', 'update_user_locale', 'Update User Locale', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'update_user_locale', 1000);
INSERT INTO "public"."required_action_provider" VALUES ('444c7461-f17f-462c-8bc9-2c097d026ace', 'webauthn-register', 'Webauthn Register', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'webauthn-register', 70);
INSERT INTO "public"."required_action_provider" VALUES ('a31a6a1a-2ac7-4f92-8d1d-5b7c12e31e90', 'webauthn-register-passwordless', 'Webauthn Register Passwordless', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'webauthn-register-passwordless', 80);
INSERT INTO "public"."required_action_provider" VALUES ('85994ee0-0b0e-408d-9b9d-bc8c3bfcd000', 'VERIFY_PROFILE', 'Verify Profile', 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 't', 'f', 'VERIFY_PROFILE', 90);
INSERT INTO "public"."required_action_provider" VALUES ('86b94414-9c94-4d01-9cfb-c37d086167cc', 'TERMS_AND_CONDITIONS', 'Terms and Conditions', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f', 'f', 'TERMS_AND_CONDITIONS', 20);
INSERT INTO "public"."required_action_provider" VALUES ('d2fa1e10-8027-4f23-8c2f-a24e4f2a48a8', 'CONFIGURE_TOTP', 'Configure OTP', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'CONFIGURE_TOTP', 10);
INSERT INTO "public"."required_action_provider" VALUES ('2e7af1bc-b1c3-4de8-9574-147278f4e145', 'UPDATE_PASSWORD', 'Update Password', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'UPDATE_PASSWORD', 30);
INSERT INTO "public"."required_action_provider" VALUES ('6652f757-7719-46c0-8551-f36ffe8ae31d', 'VERIFY_EMAIL', 'Verify Email', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'VERIFY_EMAIL', 50);
INSERT INTO "public"."required_action_provider" VALUES ('b2fffbdd-8d75-4d2e-9e89-ef6d09b2ea6d', 'UPDATE_PROFILE', 'Update Profile', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'UPDATE_PROFILE', 40);
INSERT INTO "public"."required_action_provider" VALUES ('cef5d0e6-8787-4fd5-a115-7eec8a0d653c', 'webauthn-register', 'Webauthn Register', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'webauthn-register', 70);
INSERT INTO "public"."required_action_provider" VALUES ('79fc576d-d86a-4ef5-adf6-9d50a9ac9177', 'webauthn-register-passwordless', 'Webauthn Register Passwordless', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'webauthn-register-passwordless', 80);
INSERT INTO "public"."required_action_provider" VALUES ('2b74f2b2-b647-4e33-81cd-ef7172b456de', 'VERIFY_PROFILE', 'Verify Profile', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'VERIFY_PROFILE', 90);
INSERT INTO "public"."required_action_provider" VALUES ('0b4cada9-5216-4fb8-82ef-9b4c7c9d3959', 'delete_credential', 'Delete Credential', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'delete_credential', 100);
INSERT INTO "public"."required_action_provider" VALUES ('3bd8057e-186d-499f-9757-722244a21c9e', 'update_user_locale', 'Update User Locale', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 't', 'f', 'update_user_locale', 1000);
INSERT INTO "public"."required_action_provider" VALUES ('df33bbb4-6ff6-4668-ae1d-b8be12cfd443', 'delete_account', 'Delete Account', '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'f', 'f', 'delete_account', 60);

-- ----------------------------
-- Table structure for resource_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_attribute";
CREATE TABLE "public"."resource_attribute" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'sybase-needs-something-here'::character varying,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "resource_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of resource_attribute
-- ----------------------------

-- ----------------------------
-- Table structure for resource_policy
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_policy";
CREATE TABLE "public"."resource_policy" (
  "resource_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "policy_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of resource_policy
-- ----------------------------
INSERT INTO "public"."resource_policy" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', 'cf8691be-3650-4730-8ab7-7cae73116405');
INSERT INTO "public"."resource_policy" VALUES ('bead5528-a369-4efe-877d-7da13537a9b7', '14f0a671-d39f-4631-be9b-c82e698cad94');
INSERT INTO "public"."resource_policy" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '9bf2df2c-3864-41c3-8a1e-91614452caa7');
INSERT INTO "public"."resource_policy" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '872936ed-cf13-4e72-8bae-8d1625c42929');
INSERT INTO "public"."resource_policy" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '0d93ce92-4576-46fa-9a42-e9ad4b6c77da');

-- ----------------------------
-- Table structure for resource_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_scope";
CREATE TABLE "public"."resource_scope" (
  "resource_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of resource_scope
-- ----------------------------

-- ----------------------------
-- Table structure for resource_server
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_server";
CREATE TABLE "public"."resource_server" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "allow_rs_remote_mgmt" bool NOT NULL DEFAULT false,
  "policy_enforce_mode" int2 NOT NULL,
  "decision_strategy" int2 NOT NULL DEFAULT 1
)
;

-- ----------------------------
-- Records of resource_server
-- ----------------------------
INSERT INTO "public"."resource_server" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 't', 0, 1);

-- ----------------------------
-- Table structure for resource_server_perm_ticket
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_server_perm_ticket";
CREATE TABLE "public"."resource_server_perm_ticket" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "owner" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "requester" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "created_timestamp" int8 NOT NULL,
  "granted_timestamp" int8,
  "resource_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(36) COLLATE "pg_catalog"."default",
  "resource_server_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "policy_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of resource_server_perm_ticket
-- ----------------------------

-- ----------------------------
-- Table structure for resource_server_policy
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_server_policy";
CREATE TABLE "public"."resource_server_policy" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "description" varchar(255) COLLATE "pg_catalog"."default",
  "type" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "decision_strategy" int2,
  "logic" int2,
  "resource_server_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "owner" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of resource_server_policy
-- ----------------------------
INSERT INTO "public"."resource_server_policy" VALUES ('e93d7769-c6bd-4916-a47d-bfb5ca7720a1', 'Default Permission', 'A permission that applies to the default resource type', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('edfc1154-e705-44d5-b0dd-9bc2521bb603', 'Default Policy', 'A policy that grants access only for users within this realm', 'js', 0, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('69e0cbbc-8c1b-4ca0-aa8a-1baa48c3f766', 'deny-policy', 'deny-policy', 'role', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('b816debf-bfbd-4096-82da-4df49f07047b', 'supos-default', 'supos-default', 'role', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('14f0a671-d39f-4631-be9b-c82e698cad94', 'deny-permission', 'deny-permission', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('9bf2df2c-3864-41c3-8a1e-91614452caa7', 'supos-default-permission', 'supos-default-permission', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('cf8691be-3650-4730-8ab7-7cae73116405', 'super-admin-permission', 'super-admin-permission', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('548f1a2e-ec47-4862-8195-49540ca2ad3b', 'super-admin-policy', 'super-admin-policy', 'role', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('61b4bf1a-f4bb-43a0-b91f-bbb37b1ab203', 'admin-policy', 'admin-policy', 'role', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('872936ed-cf13-4e72-8bae-8d1625c42929', 'admin-permission', 'admin-permission', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('97ad4b74-cde2-45ff-97d4-b411ac0a7153', 'normal-policy', '', 'role', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);
INSERT INTO "public"."resource_server_policy" VALUES ('0d93ce92-4576-46fa-9a42-e9ad4b6c77da', 'normal-permission', '', 'resource', 1, 0, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', NULL);

-- ----------------------------
-- Table structure for resource_server_resource
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_server_resource";
CREATE TABLE "public"."resource_server_resource" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "type" varchar(255) COLLATE "pg_catalog"."default",
  "icon_uri" varchar(255) COLLATE "pg_catalog"."default",
  "owner" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "resource_server_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "owner_managed_access" bool NOT NULL DEFAULT false,
  "display_name" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of resource_server_resource
-- ----------------------------
INSERT INTO "public"."resource_server_resource" VALUES ('db264c45-2289-4a35-a8ce-6d6f6e98753c', 'Default Resource', 'urn:supos:resources:default', NULL, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', NULL);
INSERT INTO "public"."resource_server_resource" VALUES ('bead5528-a369-4efe-877d-7da13537a9b7', 'deny-resource', 'URL', '', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', 'deny-resource');
INSERT INTO "public"."resource_server_resource" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', 'supos-default', 'URL', '', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', 'supos-default');
INSERT INTO "public"."resource_server_resource" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', 'super-admin-resource', 'URL', '', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', 'super-admin-resource');
INSERT INTO "public"."resource_server_resource" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', 'admin-resource', 'URL', '', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', 'admin-resource');
INSERT INTO "public"."resource_server_resource" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', 'normal-resource', 'URL', '', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 'f', 'normal-resource');

-- ----------------------------
-- Table structure for resource_server_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_server_scope";
CREATE TABLE "public"."resource_server_scope" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "icon_uri" varchar(255) COLLATE "pg_catalog"."default",
  "resource_server_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "display_name" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of resource_server_scope
-- ----------------------------

-- ----------------------------
-- Table structure for resource_uris
-- ----------------------------
DROP TABLE IF EXISTS "public"."resource_uris";
CREATE TABLE "public"."resource_uris" (
  "resource_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of resource_uris
-- 超管：8852fd44-b67e-4d11-9ab6-f05b9bf32f7c
-- 普通管理员：77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7
-- 一般用户：e8768c3e-2de0-40e9-8edf-fe331c539fdf
-- 默认：216ea471-fa25-441c-bbd3-3b14ae956db1
-- ----------------------------
INSERT INTO "public"."resource_uris" VALUES ('db264c45-2289-4a35-a8ce-6d6f6e98753c', '/*');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/inter-api/supos');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/AppManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/CollectionGatewayManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/NotificationManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/OpenData');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/plugin-management');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/404');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/403');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/chat2db');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/AdvancedUse');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/dashboards');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/AboutUs');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/ElasticSearch');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/CopilotKit');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/ObjectStorageServer');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/StreamProcessing');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/GenApps');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/account-management');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/open-api-docs');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/MqttBroker');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/AutoDashboard');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Grafana');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Home');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/notification-management');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/app-management');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/app-display');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', 'button:*');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Alert');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/DBConnect');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Streams');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Portainer');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/assets');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Emqx');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/grafana-design');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/DockerMgmt');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Authentication');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/alert');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/connection-flow');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Gitea');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/todo');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/advanced-use');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/EventFlow');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/McpClient');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/CICD');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/UserManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/GenerativeUI');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Logs');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/menu.account');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/flow-editor');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/home');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Dashboards');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Collection-Flow');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/DbConnect');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/CodeManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/AccountManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/collection-flow');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/SQLEditor');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/UNS');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/fuxa');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/swagger-ui');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/RoutingManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/code-management');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/SourceFlow');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/app-space');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/ContainerManagement');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Apm');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/uns');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Konga');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/Namespace');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/aboutus');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/GraphQL');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/nodered');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/open-data');
INSERT INTO "public"."resource_uris" VALUES ('8852fd44-b67e-4d11-9ab6-f05b9bf32f7c', '/WebHooks');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/McpClient');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/plugin-management');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/404');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/403');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/nodered');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/GenerativeUI');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/AboutUs');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/GenApps');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/Dashboards');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/logo');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/DbConnect');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/Home');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', 'button:*');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/Alert');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/default');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/SQLEditor');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/inter-api/supos');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/UNS');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/fuxa');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/swagger-ui');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/open-api-docs');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/assets');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/grafana-design');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/collection-gateway-management');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/CollectionGatewayManagement');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/collection-gateway-detail');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/CodeManagement');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/code-management');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/hasura');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/connection-flow');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/SourceFlow');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/app-gui');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/todo');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/Namespace');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/EventFlow');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/flow-editor');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/EvenFlowEditor');
INSERT INTO "public"."resource_uris" VALUES ('216ea471-fa25-441c-bbd3-3b14ae956db1', '/chat2db');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/collection-gateway-management');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/plugin-management');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/404');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/403');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/CollectionGatewayManagement');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/collection-gateway-detail');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/CodeManagement');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/code-management');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/AdvancedUse');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/McpClient');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/CICD');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/UserManagement');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/GenerativeUI');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Logs');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/elastic');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/AboutUs');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/ObjectStorageServer');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/GenApps');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Dashboards');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/MqttBroker');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Collection-Flow');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/DbConnect');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Home');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Alert');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', 'button:*');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/SQLEditor');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/UNS');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/fuxa');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/swagger-ui');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/open-api-docs');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/RoutingManagement');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Authentication');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/connection-flow');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/SourceFlow');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/ContainerManagement');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/todo');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Namespace');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/EventFlow');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/EvenFlowEditor');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/flow-editor');
INSERT INTO "public"."resource_uris" VALUES ('77f52ac5-58cf-4d4b-b850-aa0ac0ace1b7', '/Connection');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/CICD');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/home');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/grafana');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/McpClient');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/collection-gateway-management');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/Alert');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', 'button:*');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/collection-flow');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/SQLEditor');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/inter-api/supos');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/uns');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/DBConnect');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/EventFlow');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/CollectionGatewayManagement');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/gitea');
INSERT INTO "public"."resource_uris" VALUES ('e8768c3e-2de0-40e9-8edf-fe331c539fdf', '/grafana/home/');

-- ----------------------------
-- Table structure for revoked_token
-- ----------------------------
DROP TABLE IF EXISTS "public"."revoked_token";
CREATE TABLE "public"."revoked_token" (
  "id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "expire" int8 NOT NULL
)
;

-- ----------------------------
-- Records of revoked_token
-- ----------------------------

-- ----------------------------
-- Table structure for role_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."role_attribute";
CREATE TABLE "public"."role_attribute" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "role_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of role_attribute
-- ----------------------------

-- ----------------------------
-- Table structure for scope_mapping
-- ----------------------------
DROP TABLE IF EXISTS "public"."scope_mapping";
CREATE TABLE "public"."scope_mapping" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "role_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of scope_mapping
-- ----------------------------
INSERT INTO "public"."scope_mapping" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '6b07fca1-3f3a-438d-821b-4236995a1d27');
INSERT INTO "public"."scope_mapping" VALUES ('fd58dd90-f09d-4bd3-b7a5-e2b81440b804', '984612d6-0d73-4daa-af18-8ba04bd98970');
INSERT INTO "public"."scope_mapping" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '954a1660-6dc1-4ae5-b6f7-d2706bed7df2');
INSERT INTO "public"."scope_mapping" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', 'f9190462-32f2-4183-a192-20c1525b9b1a');

-- ----------------------------
-- Table structure for scope_policy
-- ----------------------------
DROP TABLE IF EXISTS "public"."scope_policy";
CREATE TABLE "public"."scope_policy" (
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "policy_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of scope_policy
-- ----------------------------

-- ----------------------------
-- Table structure for user_attribute
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_attribute";
CREATE TABLE "public"."user_attribute" (
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL DEFAULT 'sybase-needs-something-here'::character varying,
  "long_value_hash" bytea,
  "long_value_hash_lower_case" bytea,
  "long_value" text COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of user_attribute supos帐号ID:66b5114b-0083-48aa-860a-06f1c06ce4c4
-- ----------------------------
INSERT INTO "public"."user_attribute" VALUES ('is_temporary_admin', 'true', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22', '0aec3e55-afa5-445b-9a5c-7a9e6831ea8a', NULL, NULL, NULL);
INSERT INTO "public"."user_attribute" VALUES ('locale', 'en', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22', 'd1e522d0-bf12-44f2-9c6c-bd02142d0c8f', NULL, NULL, NULL);
INSERT INTO "public"."user_attribute" VALUES ('firstTimeLogin', '0', '66b5114b-0083-48aa-860a-06f1c06ce4c4', 'ee4aeb11-50e6-4fc6-a451-a609b7a0e2b1', NULL, NULL, NULL);
INSERT INTO "public"."user_attribute" VALUES ('tipsEnable', '0', '66b5114b-0083-48aa-860a-06f1c06ce4c4', '05a97492-1c04-49da-a3db-070981d55c97', NULL, NULL, NULL);


-- ----------------------------
-- Table structure for user_consent
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_consent";
CREATE TABLE "public"."user_consent" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "client_id" varchar(255) COLLATE "pg_catalog"."default",
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "created_date" int8,
  "last_updated_date" int8,
  "client_storage_provider" varchar(36) COLLATE "pg_catalog"."default",
  "external_client_id" varchar(255) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of user_consent
-- ----------------------------

-- ----------------------------
-- Table structure for user_consent_client_scope
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_consent_client_scope";
CREATE TABLE "public"."user_consent_client_scope" (
  "user_consent_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "scope_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_consent_client_scope
-- ----------------------------

-- ----------------------------
-- Table structure for user_entity
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_entity";
CREATE TABLE "public"."user_entity" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "email" varchar(255) COLLATE "pg_catalog"."default",
  "email_constraint" varchar(255) COLLATE "pg_catalog"."default",
  "email_verified" bool NOT NULL DEFAULT false,
  "enabled" bool NOT NULL DEFAULT false,
  "federation_link" varchar(255) COLLATE "pg_catalog"."default",
  "first_name" varchar(255) COLLATE "pg_catalog"."default",
  "last_name" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(255) COLLATE "pg_catalog"."default",
  "username" varchar(255) COLLATE "pg_catalog"."default",
  "created_timestamp" int8,
  "service_account_client_link" varchar(255) COLLATE "pg_catalog"."default",
  "not_before" int4 NOT NULL DEFAULT 0
)
;

-- ----------------------------
-- Records of user_entity
-- ----------------------------
INSERT INTO "public"."user_entity" VALUES ('0d9340a7-4bf5-4bee-9cfd-c707dfe18a22', 'yuwenhao@freezonex.io', 'yuwenhao@freezonex.io', 'f', 't', NULL, NULL, NULL, 'ef0cad76-cbcc-4c42-92d7-ef8685b7e724', 'admin', 1729679951639, NULL, 0);
INSERT INTO "public"."user_entity" VALUES ('92209a7e-1e8d-486e-9e27-92585f68e5a8', NULL, 'e4b65485-792f-4bb1-b087-f83b00435a77', 'f', 't', NULL, NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'service-account-supos', 1733364065211, 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6', 0);
INSERT INTO "public"."user_entity" VALUES ('66b5114b-0083-48aa-860a-06f1c06ce4c4', NULL, '7b2fe8c0-47f1-4bbd-bcf1-58ee824dc516', 'f', 't', NULL, NULL, NULL, '8920b375-d705-4d30-8a71-52d9c14ec4ba', 'supos', 1734059221040, NULL, 0);

-- ----------------------------
-- Table structure for user_federation_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_federation_config";
CREATE TABLE "public"."user_federation_config" (
  "user_federation_provider_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_federation_config
-- ----------------------------

-- ----------------------------
-- Table structure for user_federation_mapper
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_federation_mapper";
CREATE TABLE "public"."user_federation_mapper" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "federation_provider_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "federation_mapper_type" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_federation_mapper
-- ----------------------------

-- ----------------------------
-- Table structure for user_federation_mapper_config
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_federation_mapper_config";
CREATE TABLE "public"."user_federation_mapper_config" (
  "user_federation_mapper_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default",
  "name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_federation_mapper_config
-- ----------------------------

-- ----------------------------
-- Table structure for user_federation_provider
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_federation_provider";
CREATE TABLE "public"."user_federation_provider" (
  "id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "changed_sync_period" int4,
  "display_name" varchar(255) COLLATE "pg_catalog"."default",
  "full_sync_period" int4,
  "last_sync" int4,
  "priority" int4,
  "provider_name" varchar(255) COLLATE "pg_catalog"."default",
  "realm_id" varchar(36) COLLATE "pg_catalog"."default"
)
;

-- ----------------------------
-- Records of user_federation_provider
-- ----------------------------

-- ----------------------------
-- Table structure for user_group_membership
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_group_membership";
CREATE TABLE "public"."user_group_membership" (
  "group_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "membership_type" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_group_membership
-- ----------------------------

-- ----------------------------
-- Table structure for user_required_action
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_required_action";
CREATE TABLE "public"."user_required_action" (
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "required_action" varchar(255) COLLATE "pg_catalog"."default" NOT NULL DEFAULT ' '::character varying
)
;

-- ----------------------------
-- Records of user_required_action
-- ----------------------------

-- ----------------------------
-- Table structure for user_role_mapping
-- ----------------------------
DROP TABLE IF EXISTS "public"."user_role_mapping";
CREATE TABLE "public"."user_role_mapping" (
  "role_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "user_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of user_role_mapping

-- ----------------------------
INSERT INTO "public"."user_role_mapping" VALUES ('78bdec9a-4238-4f2f-8c9b-2d9ca2c802cc', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22');
INSERT INTO "public"."user_role_mapping" VALUES ('e9c7c988-0e1d-4ea9-8ebb-f4ddec82ca1e', '0d9340a7-4bf5-4bee-9cfd-c707dfe18a22');
INSERT INTO "public"."user_role_mapping" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '92209a7e-1e8d-486e-9e27-92585f68e5a8');
INSERT INTO "public"."user_role_mapping" VALUES ('b51711f1-2430-4a24-8493-ab1d9b0dde6f', '66b5114b-0083-48aa-860a-06f1c06ce4c4');
INSERT INTO "public"."user_role_mapping" VALUES ('7ca9f922-0d35-44cf-8747-8dcfd5e66f8e', '66b5114b-0083-48aa-860a-06f1c06ce4c4');
INSERT INTO "public"."user_role_mapping" VALUES ('09e0c927-c268-4f7e-af09-a9c46a413910', '92209a7e-1e8d-486e-9e27-92585f68e5a8');
INSERT INTO "public"."user_role_mapping" VALUES ('edac0850-db9a-4eac-8b34-3b046ecfea41', '92209a7e-1e8d-486e-9e27-92585f68e5a8');



-- ----------------------------
-- Table structure for username_login_failure
-- ----------------------------
DROP TABLE IF EXISTS "public"."username_login_failure";
CREATE TABLE "public"."username_login_failure" (
  "realm_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "username" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
  "failed_login_not_before" int4,
  "last_failure" int8,
  "last_ip_failure" varchar(255) COLLATE "pg_catalog"."default",
  "num_failures" int4
)
;

-- ----------------------------
-- Records of username_login_failure
-- ----------------------------

-- ----------------------------
-- Table structure for web_origins
-- ----------------------------
DROP TABLE IF EXISTS "public"."web_origins";
CREATE TABLE "public"."web_origins" (
  "client_id" varchar(36) COLLATE "pg_catalog"."default" NOT NULL,
  "value" varchar(255) COLLATE "pg_catalog"."default" NOT NULL
)
;

-- ----------------------------
-- Records of web_origins
-- ----------------------------
INSERT INTO "public"."web_origins" VALUES ('5d654a79-6353-4eba-8f64-a0a7c812e454', '+');
INSERT INTO "public"."web_origins" VALUES ('200c8e2f-4b1e-46e0-b1f6-92b060f2717c', '+');
INSERT INTO "public"."web_origins" VALUES ('bb5ba74b-f6b7-4eb3-aaf7-34f232f8709d', '');
INSERT INTO "public"."web_origins" VALUES ('a7b53e5e-3567-470a-9da1-94cc0c7f18e6', '*');

-- ----------------------------
-- Indexes structure for table admin_event_entity
-- ----------------------------
CREATE INDEX "idx_admin_event_time" ON "public"."admin_event_entity" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "admin_event_time" "pg_catalog"."int8_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table admin_event_entity
-- ----------------------------
ALTER TABLE "public"."admin_event_entity" ADD CONSTRAINT "constraint_admin_event_entity" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table associated_policy
-- ----------------------------
CREATE INDEX "idx_assoc_pol_assoc_pol_id" ON "public"."associated_policy" USING btree (
  "associated_policy_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table associated_policy
-- ----------------------------
ALTER TABLE "public"."associated_policy" ADD CONSTRAINT "constraint_farsrpap" PRIMARY KEY ("policy_id", "associated_policy_id");

-- ----------------------------
-- Indexes structure for table authentication_execution
-- ----------------------------
CREATE INDEX "idx_auth_exec_flow" ON "public"."authentication_execution" USING btree (
  "flow_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_auth_exec_realm_flow" ON "public"."authentication_execution" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "flow_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table authentication_execution
-- ----------------------------
ALTER TABLE "public"."authentication_execution" ADD CONSTRAINT "constraint_auth_exec_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table authentication_flow
-- ----------------------------
CREATE INDEX "idx_auth_flow_realm" ON "public"."authentication_flow" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table authentication_flow
-- ----------------------------
ALTER TABLE "public"."authentication_flow" ADD CONSTRAINT "constraint_auth_flow_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table authenticator_config
-- ----------------------------
CREATE INDEX "idx_auth_config_realm" ON "public"."authenticator_config" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table authenticator_config
-- ----------------------------
ALTER TABLE "public"."authenticator_config" ADD CONSTRAINT "constraint_auth_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table authenticator_config_entry
-- ----------------------------
ALTER TABLE "public"."authenticator_config_entry" ADD CONSTRAINT "constraint_auth_cfg_pk" PRIMARY KEY ("authenticator_id", "name");

-- ----------------------------
-- Primary Key structure for table broker_link
-- ----------------------------
ALTER TABLE "public"."broker_link" ADD CONSTRAINT "constr_broker_link_pk" PRIMARY KEY ("identity_provider", "user_id");

-- ----------------------------
-- Indexes structure for table client
-- ----------------------------
CREATE INDEX "idx_client_id" ON "public"."client" USING btree (
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table client
-- ----------------------------
ALTER TABLE "public"."client" ADD CONSTRAINT "uk_b71cjlbenv945rb6gcon438at" UNIQUE ("realm_id", "client_id");

-- ----------------------------
-- Primary Key structure for table client
-- ----------------------------
ALTER TABLE "public"."client" ADD CONSTRAINT "constraint_7" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table client_attributes
-- ----------------------------
CREATE INDEX "idx_client_att_by_name_value" ON "public"."client_attributes" USING btree (
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  substr(value, 1, 255) COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table client_attributes
-- ----------------------------
ALTER TABLE "public"."client_attributes" ADD CONSTRAINT "constraint_3c" PRIMARY KEY ("client_id", "name");

-- ----------------------------
-- Primary Key structure for table client_auth_flow_bindings
-- ----------------------------
ALTER TABLE "public"."client_auth_flow_bindings" ADD CONSTRAINT "c_cli_flow_bind" PRIMARY KEY ("client_id", "binding_name");

-- ----------------------------
-- Indexes structure for table client_initial_access
-- ----------------------------
CREATE INDEX "idx_client_init_acc_realm" ON "public"."client_initial_access" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table client_initial_access
-- ----------------------------
ALTER TABLE "public"."client_initial_access" ADD CONSTRAINT "cnstr_client_init_acc_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table client_node_registrations
-- ----------------------------
ALTER TABLE "public"."client_node_registrations" ADD CONSTRAINT "constraint_84" PRIMARY KEY ("client_id", "name");

-- ----------------------------
-- Indexes structure for table client_scope
-- ----------------------------
CREATE INDEX "idx_realm_clscope" ON "public"."client_scope" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table client_scope
-- ----------------------------
ALTER TABLE "public"."client_scope" ADD CONSTRAINT "uk_cli_scope" UNIQUE ("realm_id", "name");

-- ----------------------------
-- Primary Key structure for table client_scope
-- ----------------------------
ALTER TABLE "public"."client_scope" ADD CONSTRAINT "pk_cli_template" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table client_scope_attributes
-- ----------------------------
CREATE INDEX "idx_clscope_attrs" ON "public"."client_scope_attributes" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table client_scope_attributes
-- ----------------------------
ALTER TABLE "public"."client_scope_attributes" ADD CONSTRAINT "pk_cl_tmpl_attr" PRIMARY KEY ("scope_id", "name");

-- ----------------------------
-- Indexes structure for table client_scope_client
-- ----------------------------
CREATE INDEX "idx_cl_clscope" ON "public"."client_scope_client" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_clscope_cl" ON "public"."client_scope_client" USING btree (
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table client_scope_client
-- ----------------------------
ALTER TABLE "public"."client_scope_client" ADD CONSTRAINT "c_cli_scope_bind" PRIMARY KEY ("client_id", "scope_id");

-- ----------------------------
-- Indexes structure for table client_scope_role_mapping
-- ----------------------------
CREATE INDEX "idx_clscope_role" ON "public"."client_scope_role_mapping" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_role_clscope" ON "public"."client_scope_role_mapping" USING btree (
  "role_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table client_scope_role_mapping
-- ----------------------------
ALTER TABLE "public"."client_scope_role_mapping" ADD CONSTRAINT "pk_template_scope" PRIMARY KEY ("scope_id", "role_id");

-- ----------------------------
-- Indexes structure for table component
-- ----------------------------
CREATE INDEX "idx_component_provider_type" ON "public"."component" USING btree (
  "provider_type" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_component_realm" ON "public"."component" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table component
-- ----------------------------
ALTER TABLE "public"."component" ADD CONSTRAINT "constr_component_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table component_config
-- ----------------------------
CREATE INDEX "idx_compo_config_compo" ON "public"."component_config" USING btree (
  "component_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table component_config
-- ----------------------------
ALTER TABLE "public"."component_config" ADD CONSTRAINT "constr_component_config_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table composite_role
-- ----------------------------
CREATE INDEX "idx_composite" ON "public"."composite_role" USING btree (
  "composite" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_composite_child" ON "public"."composite_role" USING btree (
  "child_role" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table composite_role
-- ----------------------------
ALTER TABLE "public"."composite_role" ADD CONSTRAINT "constraint_composite_role" PRIMARY KEY ("composite", "child_role");

-- ----------------------------
-- Indexes structure for table credential
-- ----------------------------
CREATE INDEX "idx_user_credential" ON "public"."credential" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table credential
-- ----------------------------
ALTER TABLE "public"."credential" ADD CONSTRAINT "constraint_f" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table databasechangeloglock
-- ----------------------------
ALTER TABLE "public"."databasechangeloglock" ADD CONSTRAINT "databasechangeloglock_pkey" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table default_client_scope
-- ----------------------------
CREATE INDEX "idx_defcls_realm" ON "public"."default_client_scope" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_defcls_scope" ON "public"."default_client_scope" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table default_client_scope
-- ----------------------------
ALTER TABLE "public"."default_client_scope" ADD CONSTRAINT "r_def_cli_scope_bind" PRIMARY KEY ("realm_id", "scope_id");

-- ----------------------------
-- Indexes structure for table event_entity
-- ----------------------------
CREATE INDEX "idx_event_time" ON "public"."event_entity" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "event_time" "pg_catalog"."int8_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table event_entity
-- ----------------------------
ALTER TABLE "public"."event_entity" ADD CONSTRAINT "constraint_4" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table fed_user_attribute
-- ----------------------------
CREATE INDEX "fed_user_attr_long_values" ON "public"."fed_user_attribute" USING btree (
  "long_value_hash" "pg_catalog"."bytea_ops" ASC NULLS LAST,
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "fed_user_attr_long_values_lower_case" ON "public"."fed_user_attribute" USING btree (
  "long_value_hash_lower_case" "pg_catalog"."bytea_ops" ASC NULLS LAST,
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_attribute" ON "public"."fed_user_attribute" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_attribute
-- ----------------------------
ALTER TABLE "public"."fed_user_attribute" ADD CONSTRAINT "constr_fed_user_attr_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table fed_user_consent
-- ----------------------------
CREATE INDEX "idx_fu_cnsnt_ext" ON "public"."fed_user_consent" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "client_storage_provider" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "external_client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_consent" ON "public"."fed_user_consent" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_consent_ru" ON "public"."fed_user_consent" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_consent
-- ----------------------------
ALTER TABLE "public"."fed_user_consent" ADD CONSTRAINT "constr_fed_user_consent_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table fed_user_consent_cl_scope
-- ----------------------------
ALTER TABLE "public"."fed_user_consent_cl_scope" ADD CONSTRAINT "constraint_fgrntcsnt_clsc_pm" PRIMARY KEY ("user_consent_id", "scope_id");

-- ----------------------------
-- Indexes structure for table fed_user_credential
-- ----------------------------
CREATE INDEX "idx_fu_credential" ON "public"."fed_user_credential" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "type" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_credential_ru" ON "public"."fed_user_credential" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_credential
-- ----------------------------
ALTER TABLE "public"."fed_user_credential" ADD CONSTRAINT "constr_fed_user_cred_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table fed_user_group_membership
-- ----------------------------
CREATE INDEX "idx_fu_group_membership" ON "public"."fed_user_group_membership" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "group_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_group_membership_ru" ON "public"."fed_user_group_membership" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_group_membership
-- ----------------------------
ALTER TABLE "public"."fed_user_group_membership" ADD CONSTRAINT "constr_fed_user_group" PRIMARY KEY ("group_id", "user_id");

-- ----------------------------
-- Indexes structure for table fed_user_required_action
-- ----------------------------
CREATE INDEX "idx_fu_required_action" ON "public"."fed_user_required_action" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "required_action" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_required_action_ru" ON "public"."fed_user_required_action" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_required_action
-- ----------------------------
ALTER TABLE "public"."fed_user_required_action" ADD CONSTRAINT "constr_fed_required_action" PRIMARY KEY ("required_action", "user_id");

-- ----------------------------
-- Indexes structure for table fed_user_role_mapping
-- ----------------------------
CREATE INDEX "idx_fu_role_mapping" ON "public"."fed_user_role_mapping" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "role_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fu_role_mapping_ru" ON "public"."fed_user_role_mapping" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table fed_user_role_mapping
-- ----------------------------
ALTER TABLE "public"."fed_user_role_mapping" ADD CONSTRAINT "constr_fed_user_role" PRIMARY KEY ("role_id", "user_id");

-- ----------------------------
-- Indexes structure for table federated_identity
-- ----------------------------
CREATE INDEX "idx_fedidentity_feduser" ON "public"."federated_identity" USING btree (
  "federated_user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_fedidentity_user" ON "public"."federated_identity" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table federated_identity
-- ----------------------------
ALTER TABLE "public"."federated_identity" ADD CONSTRAINT "constraint_40" PRIMARY KEY ("identity_provider", "user_id");

-- ----------------------------
-- Primary Key structure for table federated_user
-- ----------------------------
ALTER TABLE "public"."federated_user" ADD CONSTRAINT "constr_federated_user" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table group_attribute
-- ----------------------------
CREATE INDEX "idx_group_att_by_name_value" ON "public"."group_attribute" USING btree (
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  (value::character varying(250)) COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_group_attr_group" ON "public"."group_attribute" USING btree (
  "group_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table group_attribute
-- ----------------------------
ALTER TABLE "public"."group_attribute" ADD CONSTRAINT "constraint_group_attribute_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table group_role_mapping
-- ----------------------------
CREATE INDEX "idx_group_role_mapp_group" ON "public"."group_role_mapping" USING btree (
  "group_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table group_role_mapping
-- ----------------------------
ALTER TABLE "public"."group_role_mapping" ADD CONSTRAINT "constraint_group_role" PRIMARY KEY ("role_id", "group_id");

-- ----------------------------
-- Indexes structure for table identity_provider
-- ----------------------------
CREATE INDEX "idx_ident_prov_realm" ON "public"."identity_provider" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_idp_for_login" ON "public"."identity_provider" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "enabled" "pg_catalog"."bool_ops" ASC NULLS LAST,
  "link_only" "pg_catalog"."bool_ops" ASC NULLS LAST,
  "hide_on_login" "pg_catalog"."bool_ops" ASC NULLS LAST,
  "organization_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_idp_realm_org" ON "public"."identity_provider" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "organization_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table identity_provider
-- ----------------------------
ALTER TABLE "public"."identity_provider" ADD CONSTRAINT "uk_2daelwnibji49avxsrtuf6xj33" UNIQUE ("provider_alias", "realm_id");

-- ----------------------------
-- Primary Key structure for table identity_provider
-- ----------------------------
ALTER TABLE "public"."identity_provider" ADD CONSTRAINT "constraint_2b" PRIMARY KEY ("internal_id");

-- ----------------------------
-- Primary Key structure for table identity_provider_config
-- ----------------------------
ALTER TABLE "public"."identity_provider_config" ADD CONSTRAINT "constraint_d" PRIMARY KEY ("identity_provider_id", "name");

-- ----------------------------
-- Indexes structure for table identity_provider_mapper
-- ----------------------------
CREATE INDEX "idx_id_prov_mapp_realm" ON "public"."identity_provider_mapper" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table identity_provider_mapper
-- ----------------------------
ALTER TABLE "public"."identity_provider_mapper" ADD CONSTRAINT "constraint_idpm" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table idp_mapper_config
-- ----------------------------
ALTER TABLE "public"."idp_mapper_config" ADD CONSTRAINT "constraint_idpmconfig" PRIMARY KEY ("idp_mapper_id", "name");

-- ----------------------------
-- Uniques structure for table keycloak_group
-- ----------------------------
ALTER TABLE "public"."keycloak_group" ADD CONSTRAINT "sibling_names" UNIQUE ("realm_id", "parent_group", "name");

-- ----------------------------
-- Primary Key structure for table keycloak_group
-- ----------------------------
ALTER TABLE "public"."keycloak_group" ADD CONSTRAINT "constraint_group" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table keycloak_role
-- ----------------------------
CREATE INDEX "idx_keycloak_role_client" ON "public"."keycloak_role" USING btree (
  "client" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_keycloak_role_realm" ON "public"."keycloak_role" USING btree (
  "realm" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table keycloak_role
-- ----------------------------
ALTER TABLE "public"."keycloak_role" ADD CONSTRAINT "UK_J3RWUVD56ONTGSUHOGM184WW2-2" UNIQUE ("name", "client_realm_constraint");

-- ----------------------------
-- Primary Key structure for table keycloak_role
-- ----------------------------
ALTER TABLE "public"."keycloak_role" ADD CONSTRAINT "constraint_a" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table migration_model
-- ----------------------------
CREATE INDEX "idx_update_time" ON "public"."migration_model" USING btree (
  "update_time" "pg_catalog"."int8_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table migration_model
-- ----------------------------
ALTER TABLE "public"."migration_model" ADD CONSTRAINT "constraint_migmod" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table offline_client_session
-- ----------------------------
ALTER TABLE "public"."offline_client_session" ADD CONSTRAINT "constraint_offl_cl_ses_pk3" PRIMARY KEY ("user_session_id", "client_id", "client_storage_provider", "external_client_id", "offline_flag");

-- ----------------------------
-- Indexes structure for table offline_user_session
-- ----------------------------
CREATE INDEX "idx_offline_uss_by_broker_session_id" ON "public"."offline_user_session" USING btree (
  "broker_session_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_offline_uss_by_last_session_refresh" ON "public"."offline_user_session" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "offline_flag" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "last_session_refresh" "pg_catalog"."int4_ops" ASC NULLS LAST
);
CREATE INDEX "idx_offline_uss_by_user" ON "public"."offline_user_session" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "offline_flag" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table offline_user_session
-- ----------------------------
ALTER TABLE "public"."offline_user_session" ADD CONSTRAINT "constraint_offl_us_ses_pk2" PRIMARY KEY ("user_session_id", "offline_flag");

-- ----------------------------
-- Uniques structure for table org
-- ----------------------------
ALTER TABLE "public"."org" ADD CONSTRAINT "uk_org_alias" UNIQUE ("realm_id", "alias");
ALTER TABLE "public"."org" ADD CONSTRAINT "uk_org_group" UNIQUE ("group_id");
ALTER TABLE "public"."org" ADD CONSTRAINT "uk_org_name" UNIQUE ("realm_id", "name");

-- ----------------------------
-- Primary Key structure for table org
-- ----------------------------
ALTER TABLE "public"."org" ADD CONSTRAINT "ORG_pkey" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table org_domain
-- ----------------------------
CREATE INDEX "idx_org_domain_org_id" ON "public"."org_domain" USING btree (
  "org_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table org_domain
-- ----------------------------
ALTER TABLE "public"."org_domain" ADD CONSTRAINT "ORG_DOMAIN_pkey" PRIMARY KEY ("id", "name");

-- ----------------------------
-- Primary Key structure for table policy_config
-- ----------------------------
ALTER TABLE "public"."policy_config" ADD CONSTRAINT "constraint_dpc" PRIMARY KEY ("policy_id", "name");

-- ----------------------------
-- Indexes structure for table protocol_mapper
-- ----------------------------
CREATE INDEX "idx_clscope_protmap" ON "public"."protocol_mapper" USING btree (
  "client_scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_protocol_mapper_client" ON "public"."protocol_mapper" USING btree (
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table protocol_mapper
-- ----------------------------
ALTER TABLE "public"."protocol_mapper" ADD CONSTRAINT "constraint_pcm" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table protocol_mapper_config
-- ----------------------------
ALTER TABLE "public"."protocol_mapper_config" ADD CONSTRAINT "constraint_pmconfig" PRIMARY KEY ("protocol_mapper_id", "name");

-- ----------------------------
-- Indexes structure for table realm
-- ----------------------------
CREATE INDEX "idx_realm_master_adm_cli" ON "public"."realm" USING btree (
  "master_admin_client" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table realm
-- ----------------------------
ALTER TABLE "public"."realm" ADD CONSTRAINT "uk_orvsdmla56612eaefiq6wl5oi" UNIQUE ("name");

-- ----------------------------
-- Primary Key structure for table realm
-- ----------------------------
ALTER TABLE "public"."realm" ADD CONSTRAINT "constraint_4a" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table realm_attribute
-- ----------------------------
CREATE INDEX "idx_realm_attr_realm" ON "public"."realm_attribute" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table realm_attribute
-- ----------------------------
ALTER TABLE "public"."realm_attribute" ADD CONSTRAINT "constraint_9" PRIMARY KEY ("name", "realm_id");

-- ----------------------------
-- Indexes structure for table realm_default_groups
-- ----------------------------
CREATE INDEX "idx_realm_def_grp_realm" ON "public"."realm_default_groups" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table realm_default_groups
-- ----------------------------
ALTER TABLE "public"."realm_default_groups" ADD CONSTRAINT "con_group_id_def_groups" UNIQUE ("group_id");

-- ----------------------------
-- Primary Key structure for table realm_default_groups
-- ----------------------------
ALTER TABLE "public"."realm_default_groups" ADD CONSTRAINT "constr_realm_default_groups" PRIMARY KEY ("realm_id", "group_id");

-- ----------------------------
-- Indexes structure for table realm_enabled_event_types
-- ----------------------------
CREATE INDEX "idx_realm_evt_types_realm" ON "public"."realm_enabled_event_types" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table realm_enabled_event_types
-- ----------------------------
ALTER TABLE "public"."realm_enabled_event_types" ADD CONSTRAINT "constr_realm_enabl_event_types" PRIMARY KEY ("realm_id", "value");

-- ----------------------------
-- Indexes structure for table realm_events_listeners
-- ----------------------------
CREATE INDEX "idx_realm_evt_list_realm" ON "public"."realm_events_listeners" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table realm_events_listeners
-- ----------------------------
ALTER TABLE "public"."realm_events_listeners" ADD CONSTRAINT "constr_realm_events_listeners" PRIMARY KEY ("realm_id", "value");

-- ----------------------------
-- Primary Key structure for table realm_localizations
-- ----------------------------
ALTER TABLE "public"."realm_localizations" ADD CONSTRAINT "realm_localizations_pkey" PRIMARY KEY ("realm_id", "locale");

-- ----------------------------
-- Primary Key structure for table realm_required_credential
-- ----------------------------
ALTER TABLE "public"."realm_required_credential" ADD CONSTRAINT "constraint_92" PRIMARY KEY ("realm_id", "type");

-- ----------------------------
-- Primary Key structure for table realm_smtp_config
-- ----------------------------
ALTER TABLE "public"."realm_smtp_config" ADD CONSTRAINT "constraint_e" PRIMARY KEY ("realm_id", "name");

-- ----------------------------
-- Indexes structure for table realm_supported_locales
-- ----------------------------
CREATE INDEX "idx_realm_supp_local_realm" ON "public"."realm_supported_locales" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table realm_supported_locales
-- ----------------------------
ALTER TABLE "public"."realm_supported_locales" ADD CONSTRAINT "constr_realm_supported_locales" PRIMARY KEY ("realm_id", "value");

-- ----------------------------
-- Indexes structure for table redirect_uris
-- ----------------------------
CREATE INDEX "idx_redir_uri_client" ON "public"."redirect_uris" USING btree (
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table redirect_uris
-- ----------------------------
ALTER TABLE "public"."redirect_uris" ADD CONSTRAINT "constraint_redirect_uris" PRIMARY KEY ("client_id", "value");

-- ----------------------------
-- Primary Key structure for table required_action_config
-- ----------------------------
ALTER TABLE "public"."required_action_config" ADD CONSTRAINT "constraint_req_act_cfg_pk" PRIMARY KEY ("required_action_id", "name");

-- ----------------------------
-- Indexes structure for table required_action_provider
-- ----------------------------
CREATE INDEX "idx_req_act_prov_realm" ON "public"."required_action_provider" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table required_action_provider
-- ----------------------------
ALTER TABLE "public"."required_action_provider" ADD CONSTRAINT "constraint_req_act_prv_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table resource_attribute
-- ----------------------------
ALTER TABLE "public"."resource_attribute" ADD CONSTRAINT "res_attr_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table resource_policy
-- ----------------------------
CREATE INDEX "idx_res_policy_policy" ON "public"."resource_policy" USING btree (
  "policy_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table resource_policy
-- ----------------------------
ALTER TABLE "public"."resource_policy" ADD CONSTRAINT "constraint_farsrpp" PRIMARY KEY ("resource_id", "policy_id");

-- ----------------------------
-- Indexes structure for table resource_scope
-- ----------------------------
CREATE INDEX "idx_res_scope_scope" ON "public"."resource_scope" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table resource_scope
-- ----------------------------
ALTER TABLE "public"."resource_scope" ADD CONSTRAINT "constraint_farsrsp" PRIMARY KEY ("resource_id", "scope_id");

-- ----------------------------
-- Primary Key structure for table resource_server
-- ----------------------------
ALTER TABLE "public"."resource_server" ADD CONSTRAINT "pk_resource_server" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table resource_server_perm_ticket
-- ----------------------------
CREATE INDEX "idx_perm_ticket_owner" ON "public"."resource_server_perm_ticket" USING btree (
  "owner" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_perm_ticket_requester" ON "public"."resource_server_perm_ticket" USING btree (
  "requester" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table resource_server_perm_ticket
-- ----------------------------
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "uk_frsr6t700s9v50bu18ws5pmt" UNIQUE ("owner", "requester", "resource_server_id", "resource_id", "scope_id");

-- ----------------------------
-- Primary Key structure for table resource_server_perm_ticket
-- ----------------------------
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "constraint_fapmt" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table resource_server_policy
-- ----------------------------
CREATE INDEX "idx_res_serv_pol_res_serv" ON "public"."resource_server_policy" USING btree (
  "resource_server_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table resource_server_policy
-- ----------------------------
ALTER TABLE "public"."resource_server_policy" ADD CONSTRAINT "uk_frsrpt700s9v50bu18ws5ha6" UNIQUE ("name", "resource_server_id");

-- ----------------------------
-- Primary Key structure for table resource_server_policy
-- ----------------------------
ALTER TABLE "public"."resource_server_policy" ADD CONSTRAINT "constraint_farsrp" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table resource_server_resource
-- ----------------------------
CREATE INDEX "idx_res_srv_res_res_srv" ON "public"."resource_server_resource" USING btree (
  "resource_server_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table resource_server_resource
-- ----------------------------
ALTER TABLE "public"."resource_server_resource" ADD CONSTRAINT "uk_frsr6t700s9v50bu18ws5ha6" UNIQUE ("name", "owner", "resource_server_id");

-- ----------------------------
-- Primary Key structure for table resource_server_resource
-- ----------------------------
ALTER TABLE "public"."resource_server_resource" ADD CONSTRAINT "constraint_farsr" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table resource_server_scope
-- ----------------------------
CREATE INDEX "idx_res_srv_scope_res_srv" ON "public"."resource_server_scope" USING btree (
  "resource_server_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table resource_server_scope
-- ----------------------------
ALTER TABLE "public"."resource_server_scope" ADD CONSTRAINT "uk_frsrst700s9v50bu18ws5ha6" UNIQUE ("name", "resource_server_id");

-- ----------------------------
-- Primary Key structure for table resource_server_scope
-- ----------------------------
ALTER TABLE "public"."resource_server_scope" ADD CONSTRAINT "constraint_farsrs" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table resource_uris
-- ----------------------------
ALTER TABLE "public"."resource_uris" ADD CONSTRAINT "constraint_resour_uris_pk" PRIMARY KEY ("resource_id", "value");

-- ----------------------------
-- Indexes structure for table revoked_token
-- ----------------------------
CREATE INDEX "idx_rev_token_on_expire" ON "public"."revoked_token" USING btree (
  "expire" "pg_catalog"."int8_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table revoked_token
-- ----------------------------
ALTER TABLE "public"."revoked_token" ADD CONSTRAINT "constraint_rt" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table role_attribute
-- ----------------------------
CREATE INDEX "idx_role_attribute" ON "public"."role_attribute" USING btree (
  "role_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table role_attribute
-- ----------------------------
ALTER TABLE "public"."role_attribute" ADD CONSTRAINT "constraint_role_attribute_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table scope_mapping
-- ----------------------------
CREATE INDEX "idx_scope_mapping_role" ON "public"."scope_mapping" USING btree (
  "role_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table scope_mapping
-- ----------------------------
ALTER TABLE "public"."scope_mapping" ADD CONSTRAINT "constraint_81" PRIMARY KEY ("client_id", "role_id");

-- ----------------------------
-- Indexes structure for table scope_policy
-- ----------------------------
CREATE INDEX "idx_scope_policy_policy" ON "public"."scope_policy" USING btree (
  "policy_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table scope_policy
-- ----------------------------
ALTER TABLE "public"."scope_policy" ADD CONSTRAINT "constraint_farsrsps" PRIMARY KEY ("scope_id", "policy_id");

-- ----------------------------
-- Indexes structure for table user_attribute
-- ----------------------------
CREATE INDEX "idx_user_attribute" ON "public"."user_attribute" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_user_attribute_name" ON "public"."user_attribute" USING btree (
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "value" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "user_attr_long_values" ON "public"."user_attribute" USING btree (
  "long_value_hash" "pg_catalog"."bytea_ops" ASC NULLS LAST,
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "user_attr_long_values_lower_case" ON "public"."user_attribute" USING btree (
  "long_value_hash_lower_case" "pg_catalog"."bytea_ops" ASC NULLS LAST,
  "name" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_attribute
-- ----------------------------
ALTER TABLE "public"."user_attribute" ADD CONSTRAINT "constraint_user_attribute_pk" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table user_consent
-- ----------------------------
CREATE INDEX "idx_user_consent" ON "public"."user_consent" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table user_consent
-- ----------------------------
ALTER TABLE "public"."user_consent" ADD CONSTRAINT "uk_external_consent" UNIQUE ("client_storage_provider", "external_client_id", "user_id");
ALTER TABLE "public"."user_consent" ADD CONSTRAINT "uk_local_consent" UNIQUE ("client_id", "user_id");

-- ----------------------------
-- Primary Key structure for table user_consent
-- ----------------------------
ALTER TABLE "public"."user_consent" ADD CONSTRAINT "constraint_grntcsnt_pm" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table user_consent_client_scope
-- ----------------------------
CREATE INDEX "idx_usconsent_clscope" ON "public"."user_consent_client_scope" USING btree (
  "user_consent_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_usconsent_scope_id" ON "public"."user_consent_client_scope" USING btree (
  "scope_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_consent_client_scope
-- ----------------------------
ALTER TABLE "public"."user_consent_client_scope" ADD CONSTRAINT "constraint_grntcsnt_clsc_pm" PRIMARY KEY ("user_consent_id", "scope_id");

-- ----------------------------
-- Indexes structure for table user_entity
-- ----------------------------
CREATE INDEX "idx_user_email" ON "public"."user_entity" USING btree (
  "email" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_user_service_account" ON "public"."user_entity" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
  "service_account_client_link" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Uniques structure for table user_entity
-- ----------------------------
ALTER TABLE "public"."user_entity" ADD CONSTRAINT "uk_dykn684sl8up1crfei6eckhd7" UNIQUE ("realm_id", "email_constraint");
ALTER TABLE "public"."user_entity" ADD CONSTRAINT "uk_ru8tt6t700s9v50bu18ws5ha6" UNIQUE ("realm_id", "username");

-- ----------------------------
-- Primary Key structure for table user_entity
-- ----------------------------
ALTER TABLE "public"."user_entity" ADD CONSTRAINT "constraint_fb" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table user_federation_config
-- ----------------------------
ALTER TABLE "public"."user_federation_config" ADD CONSTRAINT "constraint_f9" PRIMARY KEY ("user_federation_provider_id", "name");

-- ----------------------------
-- Indexes structure for table user_federation_mapper
-- ----------------------------
CREATE INDEX "idx_usr_fed_map_fed_prv" ON "public"."user_federation_mapper" USING btree (
  "federation_provider_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);
CREATE INDEX "idx_usr_fed_map_realm" ON "public"."user_federation_mapper" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_federation_mapper
-- ----------------------------
ALTER TABLE "public"."user_federation_mapper" ADD CONSTRAINT "constraint_fedmapperpm" PRIMARY KEY ("id");

-- ----------------------------
-- Primary Key structure for table user_federation_mapper_config
-- ----------------------------
ALTER TABLE "public"."user_federation_mapper_config" ADD CONSTRAINT "constraint_fedmapper_cfg_pm" PRIMARY KEY ("user_federation_mapper_id", "name");

-- ----------------------------
-- Indexes structure for table user_federation_provider
-- ----------------------------
CREATE INDEX "idx_usr_fed_prv_realm" ON "public"."user_federation_provider" USING btree (
  "realm_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_federation_provider
-- ----------------------------
ALTER TABLE "public"."user_federation_provider" ADD CONSTRAINT "constraint_5c" PRIMARY KEY ("id");

-- ----------------------------
-- Indexes structure for table user_group_membership
-- ----------------------------
CREATE INDEX "idx_user_group_mapping" ON "public"."user_group_membership" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_group_membership
-- ----------------------------
ALTER TABLE "public"."user_group_membership" ADD CONSTRAINT "constraint_user_group" PRIMARY KEY ("group_id", "user_id");

-- ----------------------------
-- Indexes structure for table user_required_action
-- ----------------------------
CREATE INDEX "idx_user_reqactions" ON "public"."user_required_action" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_required_action
-- ----------------------------
ALTER TABLE "public"."user_required_action" ADD CONSTRAINT "constraint_required_action" PRIMARY KEY ("required_action", "user_id");

-- ----------------------------
-- Indexes structure for table user_role_mapping
-- ----------------------------
CREATE INDEX "idx_user_role_mapping" ON "public"."user_role_mapping" USING btree (
  "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table user_role_mapping
-- ----------------------------
ALTER TABLE "public"."user_role_mapping" ADD CONSTRAINT "constraint_c" PRIMARY KEY ("role_id", "user_id");

-- ----------------------------
-- Primary Key structure for table username_login_failure
-- ----------------------------
ALTER TABLE "public"."username_login_failure" ADD CONSTRAINT "CONSTRAINT_17-2" PRIMARY KEY ("realm_id", "username");

-- ----------------------------
-- Indexes structure for table web_origins
-- ----------------------------
CREATE INDEX "idx_web_orig_client" ON "public"."web_origins" USING btree (
  "client_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
);

-- ----------------------------
-- Primary Key structure for table web_origins
-- ----------------------------
ALTER TABLE "public"."web_origins" ADD CONSTRAINT "constraint_web_origins" PRIMARY KEY ("client_id", "value");

-- ----------------------------
-- Foreign Keys structure for table associated_policy
-- ----------------------------
ALTER TABLE "public"."associated_policy" ADD CONSTRAINT "fk_frsr5s213xcx4wnkog82ssrfy" FOREIGN KEY ("associated_policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."associated_policy" ADD CONSTRAINT "fk_frsrpas14xcx4wnkog82ssrfy" FOREIGN KEY ("policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table authentication_execution
-- ----------------------------
ALTER TABLE "public"."authentication_execution" ADD CONSTRAINT "fk_auth_exec_flow" FOREIGN KEY ("flow_id") REFERENCES "public"."authentication_flow" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."authentication_execution" ADD CONSTRAINT "fk_auth_exec_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table authentication_flow
-- ----------------------------
ALTER TABLE "public"."authentication_flow" ADD CONSTRAINT "fk_auth_flow_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table authenticator_config
-- ----------------------------
ALTER TABLE "public"."authenticator_config" ADD CONSTRAINT "fk_auth_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table client_attributes
-- ----------------------------
ALTER TABLE "public"."client_attributes" ADD CONSTRAINT "fk3c47c64beacca966" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table client_initial_access
-- ----------------------------
ALTER TABLE "public"."client_initial_access" ADD CONSTRAINT "fk_client_init_acc_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table client_node_registrations
-- ----------------------------
ALTER TABLE "public"."client_node_registrations" ADD CONSTRAINT "fk4129723ba992f594" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table client_scope_attributes
-- ----------------------------
ALTER TABLE "public"."client_scope_attributes" ADD CONSTRAINT "fk_cl_scope_attr_scope" FOREIGN KEY ("scope_id") REFERENCES "public"."client_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table client_scope_role_mapping
-- ----------------------------
ALTER TABLE "public"."client_scope_role_mapping" ADD CONSTRAINT "fk_cl_scope_rm_scope" FOREIGN KEY ("scope_id") REFERENCES "public"."client_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table component
-- ----------------------------
ALTER TABLE "public"."component" ADD CONSTRAINT "fk_component_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table component_config
-- ----------------------------
ALTER TABLE "public"."component_config" ADD CONSTRAINT "fk_component_config" FOREIGN KEY ("component_id") REFERENCES "public"."component" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table composite_role
-- ----------------------------
ALTER TABLE "public"."composite_role" ADD CONSTRAINT "fk_a63wvekftu8jo1pnj81e7mce2" FOREIGN KEY ("composite") REFERENCES "public"."keycloak_role" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."composite_role" ADD CONSTRAINT "fk_gr7thllb9lu8q4vqa4524jjy8" FOREIGN KEY ("child_role") REFERENCES "public"."keycloak_role" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table credential
-- ----------------------------
ALTER TABLE "public"."credential" ADD CONSTRAINT "fk_pfyr0glasqyl0dei3kl69r6v0" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table default_client_scope
-- ----------------------------
ALTER TABLE "public"."default_client_scope" ADD CONSTRAINT "fk_r_def_cli_scope_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table federated_identity
-- ----------------------------
ALTER TABLE "public"."federated_identity" ADD CONSTRAINT "fk404288b92ef007a6" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table group_attribute
-- ----------------------------
ALTER TABLE "public"."group_attribute" ADD CONSTRAINT "fk_group_attribute_group" FOREIGN KEY ("group_id") REFERENCES "public"."keycloak_group" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table group_role_mapping
-- ----------------------------
ALTER TABLE "public"."group_role_mapping" ADD CONSTRAINT "fk_group_role_group" FOREIGN KEY ("group_id") REFERENCES "public"."keycloak_group" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table identity_provider
-- ----------------------------
ALTER TABLE "public"."identity_provider" ADD CONSTRAINT "fk2b4ebc52ae5c3b34" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table identity_provider_config
-- ----------------------------
ALTER TABLE "public"."identity_provider_config" ADD CONSTRAINT "fkdc4897cf864c4e43" FOREIGN KEY ("identity_provider_id") REFERENCES "public"."identity_provider" ("internal_id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table identity_provider_mapper
-- ----------------------------
ALTER TABLE "public"."identity_provider_mapper" ADD CONSTRAINT "fk_idpm_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table idp_mapper_config
-- ----------------------------
ALTER TABLE "public"."idp_mapper_config" ADD CONSTRAINT "fk_idpmconfig" FOREIGN KEY ("idp_mapper_id") REFERENCES "public"."identity_provider_mapper" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table keycloak_role
-- ----------------------------
ALTER TABLE "public"."keycloak_role" ADD CONSTRAINT "fk_6vyqfe4cn4wlq8r6kt5vdsj5c" FOREIGN KEY ("realm") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table policy_config
-- ----------------------------
ALTER TABLE "public"."policy_config" ADD CONSTRAINT "fkdc34197cf864c4e43" FOREIGN KEY ("policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table protocol_mapper
-- ----------------------------
ALTER TABLE "public"."protocol_mapper" ADD CONSTRAINT "fk_cli_scope_mapper" FOREIGN KEY ("client_scope_id") REFERENCES "public"."client_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."protocol_mapper" ADD CONSTRAINT "fk_pcm_realm" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table protocol_mapper_config
-- ----------------------------
ALTER TABLE "public"."protocol_mapper_config" ADD CONSTRAINT "fk_pmconfig" FOREIGN KEY ("protocol_mapper_id") REFERENCES "public"."protocol_mapper" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_attribute
-- ----------------------------
ALTER TABLE "public"."realm_attribute" ADD CONSTRAINT "fk_8shxd6l3e9atqukacxgpffptw" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_default_groups
-- ----------------------------
ALTER TABLE "public"."realm_default_groups" ADD CONSTRAINT "fk_def_groups_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_enabled_event_types
-- ----------------------------
ALTER TABLE "public"."realm_enabled_event_types" ADD CONSTRAINT "fk_h846o4h0w8epx5nwedrf5y69j" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_events_listeners
-- ----------------------------
ALTER TABLE "public"."realm_events_listeners" ADD CONSTRAINT "fk_h846o4h0w8epx5nxev9f5y69j" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_required_credential
-- ----------------------------
ALTER TABLE "public"."realm_required_credential" ADD CONSTRAINT "fk_5hg65lybevavkqfki3kponh9v" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_smtp_config
-- ----------------------------
ALTER TABLE "public"."realm_smtp_config" ADD CONSTRAINT "fk_70ej8xdxgxd0b9hh6180irr0o" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table realm_supported_locales
-- ----------------------------
ALTER TABLE "public"."realm_supported_locales" ADD CONSTRAINT "fk_supported_locales_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table redirect_uris
-- ----------------------------
ALTER TABLE "public"."redirect_uris" ADD CONSTRAINT "fk_1burs8pb4ouj97h5wuppahv9f" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table required_action_provider
-- ----------------------------
ALTER TABLE "public"."required_action_provider" ADD CONSTRAINT "fk_req_act_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_attribute
-- ----------------------------
ALTER TABLE "public"."resource_attribute" ADD CONSTRAINT "fk_5hrm2vlf9ql5fu022kqepovbr" FOREIGN KEY ("resource_id") REFERENCES "public"."resource_server_resource" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_policy
-- ----------------------------
ALTER TABLE "public"."resource_policy" ADD CONSTRAINT "fk_frsrpos53xcx4wnkog82ssrfy" FOREIGN KEY ("resource_id") REFERENCES "public"."resource_server_resource" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."resource_policy" ADD CONSTRAINT "fk_frsrpp213xcx4wnkog82ssrfy" FOREIGN KEY ("policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_scope
-- ----------------------------
ALTER TABLE "public"."resource_scope" ADD CONSTRAINT "fk_frsrpos13xcx4wnkog82ssrfy" FOREIGN KEY ("resource_id") REFERENCES "public"."resource_server_resource" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."resource_scope" ADD CONSTRAINT "fk_frsrps213xcx4wnkog82ssrfy" FOREIGN KEY ("scope_id") REFERENCES "public"."resource_server_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_server_perm_ticket
-- ----------------------------
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "fk_frsrho213xcx4wnkog82sspmt" FOREIGN KEY ("resource_server_id") REFERENCES "public"."resource_server" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "fk_frsrho213xcx4wnkog83sspmt" FOREIGN KEY ("resource_id") REFERENCES "public"."resource_server_resource" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "fk_frsrho213xcx4wnkog84sspmt" FOREIGN KEY ("scope_id") REFERENCES "public"."resource_server_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."resource_server_perm_ticket" ADD CONSTRAINT "fk_frsrpo2128cx4wnkog82ssrfy" FOREIGN KEY ("policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_server_policy
-- ----------------------------
ALTER TABLE "public"."resource_server_policy" ADD CONSTRAINT "fk_frsrpo213xcx4wnkog82ssrfy" FOREIGN KEY ("resource_server_id") REFERENCES "public"."resource_server" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_server_resource
-- ----------------------------
ALTER TABLE "public"."resource_server_resource" ADD CONSTRAINT "fk_frsrho213xcx4wnkog82ssrfy" FOREIGN KEY ("resource_server_id") REFERENCES "public"."resource_server" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_server_scope
-- ----------------------------
ALTER TABLE "public"."resource_server_scope" ADD CONSTRAINT "fk_frsrso213xcx4wnkog82ssrfy" FOREIGN KEY ("resource_server_id") REFERENCES "public"."resource_server" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table resource_uris
-- ----------------------------
ALTER TABLE "public"."resource_uris" ADD CONSTRAINT "fk_resource_server_uris" FOREIGN KEY ("resource_id") REFERENCES "public"."resource_server_resource" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table role_attribute
-- ----------------------------
ALTER TABLE "public"."role_attribute" ADD CONSTRAINT "fk_role_attribute_id" FOREIGN KEY ("role_id") REFERENCES "public"."keycloak_role" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table scope_mapping
-- ----------------------------
ALTER TABLE "public"."scope_mapping" ADD CONSTRAINT "fk_ouse064plmlr732lxjcn1q5f1" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table scope_policy
-- ----------------------------
ALTER TABLE "public"."scope_policy" ADD CONSTRAINT "fk_frsrasp13xcx4wnkog82ssrfy" FOREIGN KEY ("policy_id") REFERENCES "public"."resource_server_policy" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."scope_policy" ADD CONSTRAINT "fk_frsrpass3xcx4wnkog82ssrfy" FOREIGN KEY ("scope_id") REFERENCES "public"."resource_server_scope" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_attribute
-- ----------------------------
ALTER TABLE "public"."user_attribute" ADD CONSTRAINT "fk_5hrm2vlf9ql5fu043kqepovbr" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_consent
-- ----------------------------
ALTER TABLE "public"."user_consent" ADD CONSTRAINT "fk_grntcsnt_user" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_consent_client_scope
-- ----------------------------
ALTER TABLE "public"."user_consent_client_scope" ADD CONSTRAINT "fk_grntcsnt_clsc_usc" FOREIGN KEY ("user_consent_id") REFERENCES "public"."user_consent" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_federation_config
-- ----------------------------
ALTER TABLE "public"."user_federation_config" ADD CONSTRAINT "fk_t13hpu1j94r2ebpekr39x5eu5" FOREIGN KEY ("user_federation_provider_id") REFERENCES "public"."user_federation_provider" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_federation_mapper
-- ----------------------------
ALTER TABLE "public"."user_federation_mapper" ADD CONSTRAINT "fk_fedmapperpm_fedprv" FOREIGN KEY ("federation_provider_id") REFERENCES "public"."user_federation_provider" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "public"."user_federation_mapper" ADD CONSTRAINT "fk_fedmapperpm_realm" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_federation_mapper_config
-- ----------------------------
ALTER TABLE "public"."user_federation_mapper_config" ADD CONSTRAINT "fk_fedmapper_cfg" FOREIGN KEY ("user_federation_mapper_id") REFERENCES "public"."user_federation_mapper" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_federation_provider
-- ----------------------------
ALTER TABLE "public"."user_federation_provider" ADD CONSTRAINT "fk_1fj32f6ptolw2qy60cd8n01e8" FOREIGN KEY ("realm_id") REFERENCES "public"."realm" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_group_membership
-- ----------------------------
ALTER TABLE "public"."user_group_membership" ADD CONSTRAINT "fk_user_group_user" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_required_action
-- ----------------------------
ALTER TABLE "public"."user_required_action" ADD CONSTRAINT "fk_6qj3w1jw9cvafhe19bwsiuvmd" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table user_role_mapping
-- ----------------------------
ALTER TABLE "public"."user_role_mapping" ADD CONSTRAINT "fk_c4fqv34p1mbylloxang7b1q3l" FOREIGN KEY ("user_id") REFERENCES "public"."user_entity" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

-- ----------------------------
-- Foreign Keys structure for table web_origins
-- ----------------------------
ALTER TABLE "public"."web_origins" ADD CONSTRAINT "fk_lojpho213xcx4wnkog82ssrfy" FOREIGN KEY ("client_id") REFERENCES "public"."client" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
