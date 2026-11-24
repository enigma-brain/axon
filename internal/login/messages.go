package login

// The text axon login prints when it makes a pathway or asks about
// replacing a stored one, in one place. The %-verbs are filled in the order
// each comment gives.

// Making a pathway at the prompt.
const (
	msgPromptIntro  = "No pathway is set up on this laptop yet. Answer a few questions to make one, or press Ctrl-C and run `axon login --config <source>` with the file your organization gives you.\n"
	msgKindQuestion = "How do you reach the SkyPilot API server?\n" +
		"  1) Directly, at its address\n" +
		"  2) Through Teleport\n"
	msgKindPrompt = "Kind [1-2]: "
	// The answer.
	msgNotAKind = "%q is not one of the kinds.\n"

	msgEndpointPrompt = "SkyPilot API server address, such as https://sky.example.org: "
	msgProxyPrompt    = "Teleport proxy address, such as teleport.example.org: "
	// The default application.
	msgAppPrompt = "Teleport application that serves SkyPilot [Enter for %s]: "
	// The default name.
	msgNamePrompt = "Name for this pathway [Enter for %s]: "

	// The answer, for each.
	msgNotHTTPS    = "%q is not an https address."
	msgNotHostPort = "%q is not a host or host:port."
	msgNotApp      = "%q is not a Teleport application name."
	msgNameEmpty   = "The name needs at least one letter or digit."
	msgNameUnsafe  = "The name must not hold control characters or Unicode format characters."

	// The address.
	msgChecking = "Checking %s\n"
	// The address and the reason, for each.
	msgSkyUnreachable   = "Can't reach the SkyPilot API server at %s: %s. Check the address and try again."
	msgSkyNotSkyPilot   = "%s answered, but not as a SkyPilot API server (%s). Check the address and try again."
	msgProxyUnreachable = "Can't reach the Teleport proxy at %s: %s. Check the address and try again."

	msgNoPathwayMade = "no pathway made. Run `axon login` again on a terminal, or with --sky-endpoint, --teleport-proxy or --config."

	// The name and the stored file's path.
	msgStoredPathway = "Stored the pathway %q in %s\n"
)

// Making a pathway from flags. The address and the reason, for each.
const (
	msgFlagSkyUnreachable   = "can't reach the SkyPilot API server at %s: %s. Check the address and run `axon login` again."
	msgFlagSkyNotSkyPilot   = "%s answered, but not as a SkyPilot API server (%s). Check the address and run `axon login` again."
	msgFlagProxyUnreachable = "can't reach the Teleport proxy at %s: %s. Check the address and run `axon login` again."
	// The flag and the message about its value.
	msgBadFlag = "--%s: %s"
)

// What a hand-made file says about its pathway, in the list of pathways.
const (
	// The address.
	descDirect = "Made on this laptop. Direct to %s."
	// The proxy and the application.
	descTeleport = "Made on this laptop. Through Teleport at %s, application %s."
)

// Nothing stored, and no terminal to ask on.
const msgNoPathwayNoTerminal = "no pathway is set up, and there is no terminal to ask on. Run `axon login --sky-endpoint <address>` for a SkyPilot API server you reach directly, `axon login --teleport-proxy <address>` for one through Teleport, or `axon login --config <source>` with the file your organization gives you."

// Flags that contradict each other.
const (
	msgEndpointAndProxy = "--sky-endpoint and --teleport-proxy can't be used together: --sky-endpoint is for a server you reach directly, --teleport-proxy for one through Teleport."
	msgAppNeedsProxy    = "--sky-app needs --teleport-proxy."
	msgNameNeedsServer  = "--name needs --sky-endpoint or --teleport-proxy."
	// The flag, twice.
	msgConfigWith  = "--config can't be used with --%s: --config adds a config file, and --%s makes a pathway without one."
	msgPathwayWith = "--pathway can't be used with --%s: --pathway picks a stored pathway, and --%s makes a new one."
)

// Questions about replacing a stored pathway.
const (
	// The stored pathway's name, then one of the reasons below, then
	// msgClashAdding or msgClashAddingNamed.
	msgClashStored = "The stored pathway %q %s"
	msgClashName   = "has the same name"
	// The address.
	msgClashDirect = "uses the same SkyPilot API server, %s,"
	// The proxy and the application.
	msgClashTeleport    = "uses the same Teleport proxy and application, %s and %s,"
	msgClashAdding      = " as the pathway you are adding.\n"
	msgClashAddingNamed = " as the pathway %q you are adding.\n"
	// The stored file's path and its pathways' names.
	msgClashRemovesFile     = "Replacing it removes the config file %s and all its pathways: %s.\n"
	msgClashRemovesHandMade = "Replacing it removes that pathway, which was made on this laptop.\n"
	msgReplacePrompt        = "Replace it? [y/N, Enter for no]: "
	msgAnswerYesNo          = "Answer y or n.\n"

	// The stored file's path.
	msgRemovedFile = "Removed the config file %s\n"
	// The stored pathway's name.
	msgRemovedPathway = "Removed the pathway %q\n"
	msgChooseName     = "Choose another name.\n"
	msgKeepingBoth    = "Keeping both.\n"

	// The pathway's name and the stored file's path.
	msgConfigNotAdded = "not added, since its pathway %q has the same name as a stored pathway in %s. Run the command again with --replace to replace that file."
	// The added pathway's name, the stored pathway's name and the stored
	// file's path.
	msgConfigClashNoTerminal = "its pathway %q clashes with the stored pathway %q in %s, and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it."
	// The stored pathway's name and the stored file's path.
	msgPathwayClashNoTerminal = "it clashes with the stored pathway %q in %s, and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it."
)

// A config file that holds nothing.
const (
	// The source: the path, the address, or msgStdin.
	msgConfigEmpty = "%s is empty."
	msgStdin       = "standard input"
	// The stored file's path and the address it was fetched from again.
	msgRefreshEmpty = "axon: could not update %s: %s is empty. Using the stored file.\n"
)

// The next step after a stored pathway's Teleport proxy gives no answer,
// when the pathway has no unreachable_hint.
const msgProxyUnreachableNext = "Check your network connection and run `axon login` again."

// Revisions.
const (
	// The stored file's path.
	msgPutBack = "Put back the previous copy of %s, which an interrupted update had replaced.\n"

	// The stored file's path, the stored revision and the new one.
	msgLowerRevision = "The stored copy of the config file %s is revision %d, and this one is revision %d, which is older.\n"
	// The new revision.
	msgLowerPrompt = "Replace the stored copy with revision %d? [y/N, Enter for no]: "
	// The stored revision.
	msgKeptRevision = "Kept revision %d.\n"
	// The address, the new revision and the stored one.
	msgLowerRevisionWarning = "axon: the config file at %s is revision %d, older than the stored revision %d. Using the stored file.\n"

	// The stored file's path.
	msgStoredFile = "Stored the config file %s\n"
	// The stored file's path and the revision.
	msgStoredFileRevision = "Stored the config file %s (revision %d)\n"
	// The stored file's path.
	msgUpdatedFile = "Updated the config file %s\n"
	// The stored file's path, the old revision and the new one, each
	// "revision N" or "no revision".
	msgUpdatedFileRevision = "Updated the config file %s from %s to %s\n"
	// A revision number.
	msgRevisionN  = "revision %d"
	msgNoRevision = "no revision"
)

// A config file, fetched again or added with --config, that changes where
// the pathway in use goes.
const (
	// The address and the pathway's identifier.
	msgDestinationChanged = "The config file at %s changes where the pathway %q goes:\n"
	// The pathway's identifier.
	msgDestinationChangedAdded = "The config file changes where the pathway %q goes:\n"
	// Where the pathway goes, before and after.
	msgDestinationNow  = "  now: %s\n"
	msgDestinationNew  = "  new: %s\n"
	msgDestinationGone = "  new: no longer in the file\n"

	msgDestinationPrompt = "Switch to the fetched config file? [y/N, Enter for no]: "
	// The stored file's path.
	msgDestinationKept = "Kept the stored copy of %s.\n"
	// The address and the pathway's identifier.
	msgDestinationWarning = "axon: the config file at %s changes where the pathway %q goes, and there is no terminal to ask whether to switch. Using the stored file. Run `axon login --replace` to switch.\n"
)
