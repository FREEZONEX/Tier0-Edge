<#import "field.ftl" as field>
<#macro username>
  <#assign label>
    <#if !realm.loginWithEmailAllowed>${msg("username")}<#elseif !realm.registrationEmailAsUsername>${msg("usernameOrEmail")}<#else>${msg("email")}</#if>
  </#assign>
  <@field.group name="username" label=label>
    <div class="${properties.kcInputGroup}">
      <div class="${properties.kcInputGroupItemClass} ${properties.kcFill}">
        <span class="${properties.kcInputClass} ${properties.kcFormReadOnlyClass}">
          <input id="kc-attempted-username" value="${auth.attemptedUsername}" readonly>
        </span>
      </div>
      <div class="${properties.kcInputGroupItemClass}">
        <button id="reset-login" class="${properties.kcFormPasswordVisibilityButtonClass} kc-login-tooltip" type="button"
              aria-label="${msg('restartLoginTooltip')}" onclick="location.href='${url.loginRestartFlowUrl}'">
            <i class="fa-sync-alt fas" aria-hidden="true"></i>
            <span class="kc-tooltip-text">${msg("restartLoginTooltip")}</span>
        </button>
      </div> 
    </div>
  </@field.group>
</#macro>

<#macro registrationLayout bodyClass="" displayInfo=false displayMessage=true displayRequiredFields=false>
<!DOCTYPE html>
<html class="${properties.kcHtmlClass!}"<#if realm.internationalizationEnabled> lang="${locale.currentLanguageTag}" dir="${(locale.rtl)?then('rtl','ltr')}"</#if>>

<head>
    <meta charset="utf-8">
    <meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
    <meta name="robots" content="noindex, nofollow">

    <#if properties.meta?has_content>
        <#list properties.meta?split(' ') as meta>
            <meta name="${meta?split('==')[0]}" content="${meta?split('==')[1]}"/>
        </#list>
    </#if>
    <title>${msg("loginTitle",(realm.displayName!''))}</title>
    <link rel="icon" type="image/svg+xml" href="/files/system/resource/supos/logo-ico.svg" id="dynamic-favicon"/>
    <#if properties.stylesCommon?has_content>
        <#list properties.stylesCommon?split(' ') as style>
            <link href="${url.resourcesCommonPath}/${style}" rel="stylesheet" />
        </#list>
    </#if>
    <link rel="stylesheet" href="${url.resourcesPath}/css/auth.css">
    <#if properties.styles?has_content>
        <#list properties.styles?split(' ') as style>
            <link href="${url.resourcesPath}/${style}" rel="stylesheet" />
        </#list>
    </#if>
    <script type="importmap">
        {
            "imports": {
                "rfc4648": "${url.resourcesCommonPath}/vendor/rfc4648/rfc4648.js"
            }
        }
    </script>
    <#if properties.scripts?has_content>
        <#list properties.scripts?split(' ') as script>
            <script src="${url.resourcesPath}/${script}" type="text/javascript"></script>
        </#list>
    </#if>
    <#if scripts??>
        <#list scripts as script>
            <script src="${script}" type="text/javascript"></script>
        </#list>
    </#if>
    <script type="module" src="${url.resourcesPath}/js/passwordVisibility.js"></script>
    <script src="${url.resourcesPath}/js/qrcode.min.js"></script>
    <script type="module" src="${url.resourcesPath}/js/installAuth.js"></script>
    <script type="module">
        import { startSessionPolling } from "${url.resourcesPath}/js/authChecker.js";
        import { handleTheme } from "${url.resourcesPath}/js/customThemeConfig.js";
        startSessionPolling(
            "${url.ssoLoginInOtherTabsUrl?no_esc}"
        );
        const keycloakUrl = "${url.resourcesPath}";
        const lang = "${msg("suposLang")}";
        handleTheme(keycloakUrl,lang)
    </script>
</head>

<body id="keycloak-bg" class="${properties.kcBodyClass!}">

<div class="${properties.kcLogin!}">
  <div class="pf-v5-c-login-left">
    <div class="pf-v5-c-login-l-top">
      <img class="supos-login-slogan" src="" />
      <div class="pf-v5-c-login-l-t-right"></div>
    </div>
    <div class="pf-v5-c-login-l-bottom">
      <img class="supos-logo" src="" />
      <div style="display: none;">${msg("customIndustrialOperatingSystem")}</div>
    </div>
  </div>
  <div class="${properties.kcLoginContainer!}">
    <main class="${properties.kcLoginMain!}" id="main" style="display:none;">
      <div class="${properties.kcLoginMainHeader!}">
        <h1 class="${properties.kcLoginMainTitle!}" id="kc-page-title"><#nested "header"></h1>
        <#if realm.internationalizationEnabled  && locale.supported?size gt 1>
        <div class="${properties.kcLoginMainHeaderUtilities!}">
          <div class="${properties.kcInputClass!}">
            <select
              aria-label="${msg("languages")}"
              id="login-select-toggle"
              onchange="if (this.value) window.location.href=this.value"
            >
              <#list locale.supported?sort_by("label") as l>
                <option
                  value="${l.url}"
                  ${(l.languageTag == locale.currentLanguageTag)?then('selected','')}
                >
                  ${l.label}
                </option>
              </#list>
            </select>
            <span class="${properties.kcFormControlUtilClass}">
              <span class="${properties.kcFormControlToggleIcon!}">
                <svg
                  class="pf-v5-svg"
                  viewBox="0 0 320 512"
                  fill="currentColor"
                  aria-hidden="true"
                  role="img"
                  width="1em"
                  height="1em"
                >
                  <path
                    d="M31.3 192h257.3c17.8 0 26.7 21.5 14.1 34.1L174.1 354.8c-7.8 7.8-20.5 7.8-28.3 0L17.2 226.1C4.6 213.5 13.5 192 31.3 192z"
                  >
                  </path>
                </svg>
              </span>
            </span>
          </div>
        </div>
        </#if>
      </div>
      <!--<div class="kc-page-title-tip">If you are already a member you can login with your user name and password.</div>-->
      <div id="kcFormBox" class="${properties.kcLoginMainBody!}">
        <#if !(auth?has_content && auth.showUsername() && !auth.showResetCredentials())>
            <#if displayRequiredFields>
                <div class="${properties.kcContentWrapperClass!}">
                    <div class="${properties.kcLabelWrapperClass!} subtitle">
                        <span class="${properties.kcInputHelperTextItemTextClass!}">
                          <span class="${properties.kcInputRequiredClass!}">*</span> ${msg("requiredFields")}
                        </span>
                    </div>
                </div>
            </#if>
        <#else>
            <#if displayRequiredFields>
                <div class="${properties.kcContentWrapperClass!}">
                    <div class="${properties.kcLabelWrapperClass!} subtitle">
                        <span class="${properties.kcInputHelperTextItemTextClass!}">
                          <span class="${properties.kcInputRequiredClass!}">*</span> ${msg("requiredFields")}
                        </span>
                    </div>
                    <div class="${properties.kcFormClass} ${properties.kcContentWrapperClass}">
                        <#nested "show-username">
                        <@username />
                    </div>
                </div>
            <#else>
                <div class="${properties.kcFormClass} ${properties.kcContentWrapperClass}">
                  <#nested "show-username">
                  <@username />
                </div>
            </#if>
        </#if>

        <#-- App-initiated actions should not see warning messages about the need to complete the action -->
        <#-- during login.                                                                               -->
        <#if displayMessage && message?has_content && (message.type != 'warning' || !isAppInitiatedAction??)>
            <div class="${properties.kcAlertClass!} pf-m-${(message.type = 'error')?then('danger', message.type)}">
                <div class="${properties.kcAlertIconClass!}">
                    <#if message.type = 'success'><span class="${properties.kcFeedbackSuccessIcon!}"></span></#if>
                    <#if message.type = 'warning'><span class="${properties.kcFeedbackWarningIcon!}"></span></#if>
                    <#if message.type = 'error'><span class="${properties.kcFeedbackErrorIcon!}"></span></#if>
                    <#if message.type = 'info'><span class="${properties.kcFeedbackInfoIcon!}"></span></#if>
                </div>
                <span class="${properties.kcAlertTitleClass!} kc-feedback-text">${kcSanitize(message.summary)?no_esc}</span>
            </div>
        </#if>

        <#nested "form">

        <#if auth?has_content && auth.showTryAnotherWayLink()>
          <form id="kc-select-try-another-way-form" action="${url.loginAction}" method="post" novalidate="novalidate">
              <input type="hidden" name="tryAnotherWay" value="on"/>
              <a id="try-another-way" href="javascript:document.forms['kc-select-try-another-way-form'].submit()"
                  class="${properties.kcButtonSecondaryClass} ${properties.kcButtonBlockClass} ${properties.kcMarginTopClass}">
                    ${kcSanitize(msg("doTryAnotherWay"))?no_esc}
              </a>
          </form>
        </#if>

        <#if displayInfo>
          <div id="kc-info" class="${properties.kcSignUpClass!}">
              <div id="kc-info-wrapper" class="${properties.kcInfoAreaWrapperClass!}">
                  <#nested "info">
              </div>
          </div>
        </#if>
      </div>
      <div class="pf-v5-c-login__main-footer">
        <#nested "socialProviders">
      </div>
    </main>
    <div class="errorBox" id="errorBox">
      <i class="fas fa-exclamation-circle" aria-hidden="true"></i>
      <span id="errorMsgContent"></span>
    </div>
    <div class="auth-box" id="authBox" style="display:none;">
      <!-- 国际化文本容器，用于JavaScript -->
      <div id="i18n-messages" style="display:none;" 
        data-requestFailed="${msg("requestFailed")}"
        data-pleaseEnterRealPhoneNumber="${msg("pleaseEnterRealPhoneNumber")}"
        data-pleaseEnterRealEmail="${msg("pleaseEnterRealEmail")}">
      </div>
      <div class="auth-box-title"> 
        ${msg("loginLicenseAuthentication")}
      </div>
      <div class="form-item required">
        <label for="companyName"><span class="required-dot">*</span>${msg("companyName")}</label>
        <input type="text" id="companyName" name="companyName" placeholder="${msg("pleaseEnterCompanyName")}" />
        <span class="error-msg" id="companyNameError"></span>
      </div>
      <div class="form-item required">
        <label for="username"><span class="required-dot">*</span>${msg("applicant")}</label>
        <input type="text" id="username" name="username" placeholder="${msg("pleaseEnterApplicant")}" />
        <span class="error-msg" id="usernameError"></span>
      </div>
      <div class="form-item required">
        <label for="phone"><span class="required-dot">*</span>${msg("phoneNumber")}</label>
        <input type="text" id="phone" name="phone" placeholder="${msg("pleaseEnterPhoneNumber")}" />
        <span class="error-msg" id="phoneError"></span>
      </div>
      <div class="form-item required">
        <label for="email">${msg("email")}</label>
        <input type="email" id="email" name="email" placeholder="${msg("pleaseEnterEmail")}" />
        <span class="error-msg" id="emailError"></span>
      </div>
      <div class="form-item required">
        <label for="industryCode"><span class="required-dot">*</span>${msg("industry")}</label>
        <select id="industryCode" name="industryCode" >
          <option value="A">${msg("chemicalIndustry")}</option>
          <option value="B">${msg("petrify")}</option>
          <option value="C">${msg("electricity")}</option>
          <option value="D">${msg("metallurgy")}</option>
          <option value="E">${msg("buildingMaterials")}</option>
          <option value="F">${msg("papermaking")}</option>
          <option value="G">${msg("foodAndMedicine")}</option>
          <option value="H">${msg("equipmentManufacturing")}</option>
          <option value="I">${msg("automotiveParts")}</option>
          <option value="J">${msg("metalProducts")}</option>
          <option value="K">${msg("electronicAppliancesAndInstruments")}</option>
          <option value="N">${msg("teachingAndResearch")}</option>
          <option value="O">${msg("smartPark")}</option>
          <option value="P">${msg("informationSoftwareEcosystemPartner")}</option>
          <option value="Q">${msg("specialProject")}</option>
          <option value="R">${msg("utilityService")}</option>
          <option value="S">${msg("urbanInfrastructure")}</option>
          <option value="L">${msg("newMaterialsIndustry")}</option>
          <option value="M">${msg("otherIndustries")}</option>
        </select>
        <span class="error-msg" id="industryCodeError"></span>
      </div>
      <div class="form-item required" id="verifyCodeFormItem" style="display: none">
        <label for="verifyCode"><span class="required-dot">*</span>${msg("verificationCode")}</label>
        <div class="auth-code-wrapper">
          <input type="text" class="formInput auth-code-input" id="verifyCode" name="verifyCode" placeholder="${msg("pleaseEnterVerificationCode")}" style="padding-right: 116px;" />
          <span class="auth-code-suffix">
            <a class="get-code-btn" id="getCodeBtn">${msg("getVerificationCode")}</a>
          </span>
        </div>
        <span class="error-msg" id="verifyCodeError"></span>
      </div>
      <button class="button primary" id="submitAuth">${msg("confirm")}</button>
      <div class="codeErrorMsg" id="codeErrorMsg">
        <div class="divider"></div>
        <span>
          ${msg("networkErrorScanQRCode")}
        </span>
        <div class="qrImg" id="qrImg"></div>
      </div>
    </div>
    <div class="loadingWrap" id="loadingWrap" style="display:none;">
      <div class="customLoading" id="customLoading"></div>
    </div>
  </div>
</div>
</body>
</html>
</#macro>